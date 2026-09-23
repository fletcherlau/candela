// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"syncer/internal/config"
	"syncer/internal/core"
	"syncer/internal/handler"
	"syncer/internal/rotation"
	"syncer/internal/schema"
	"syncer/internal/store"
	"syncer/internal/svc"
	"syncer/internal/syncrun"

	tushare "github.com/fletcherlau/go-tushare"
	_ "github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/rest"
)

var (
	rotationRefresh = flag.Bool("rotation-refresh", false, "sync the four ETFs and publish historical rotation results")
	configFile      = flag.String("f", "etc/syncer-api.yaml", "the config file")
	once            = flag.Bool("once", false, "run a one-shot sync for all sync-enabled instruments and exit")
	onceSW          = flag.Bool("once-sw", false, "run a one-shot sync for SW industry dictionary, membership and index daily, then exit")
)

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())
	if c.MysqlDSN == "" {
		log.Fatal("MYSQL_DSN is required: set it via environment (see .env.example)")
	}
	if c.Tushare.Token == "" {
		log.Print("warn: TUSHARE_TOKEN is empty, sync will fail on upstream calls")
	}
	if c.ApiKey == "" {
		log.Print("warn: SYNC_API_KEY is not set, sync endpoints are UNAUTHENTICATED (local debug only)")
	}

	db, err := sql.Open("mysql", c.MysqlDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping mysql: %v", err)
	}
	if err := schema.Ensure(ctx, db); err != nil {
		log.Fatalf("ensure schema: %v", err)
	}

	// 限频由 go-tushare 客户端内置：任意两次 HTTP 调用间隔不低于 ThrottleMs。
	tushareClient := tushare.NewClient(c.Tushare.Token,
		tushare.WithHTTPURL(c.Tushare.BaseURL),
		tushare.WithMinInterval(time.Duration(c.Sync.ThrottleMs)*time.Millisecond),
	)
	source := &fundDailySource{client: tushareClient}
	swSrc := &swSource{client: tushareClient}
	st := store.NewMySQLStore(db)
	syncer := core.NewSyncer(source, st,
		c.Sync.ChunkDays, c.Sync.DefaultStartDate, nil)
	swSyncer := core.NewSWSyncer(swSrc, st,
		c.Sync.ChunkDays, c.Sync.DefaultStartDate, "", nil)
	// 盘中信号：gtimg 实时行情 + 库内日线，分位窗口与 today 用默认值。
	signalComputer := core.NewSignalComputer(newGtimgSource(""), st, 0, nil)

	rotationService := &rotation.Service{DB: db, Calendar: &rotationCalendar{client: tushareClient}, Realtime: newGtimgSource("")}
	syncer.SetObserver(rotationService)
	if *rotationRefresh {
		sum := syncer.Run(context.Background(), core.RotationCodes)
		if sum.Success != sum.Total {
			log.Fatal("four ETF sync incomplete")
		}
		if err := rotationService.Refresh(context.Background()); err != nil {
			log.Fatal("rotation refresh failed")
		}
		if err := rotationService.RefreshDaily(context.Background()); err != nil {
			log.Fatal("daily rotation refresh failed")
		}
		fmt.Println("Four ETF synchronization and rotation refresh finished")
		return
	}
	if *once {
		sum := syncer.Run(context.Background(), nil)
		out, _ := json.MarshalIndent(sum, "", "  ")
		fmt.Println(string(out))
		if sum.Success != sum.Total {
			log.Fatalf("one-shot sync incomplete: %d/%d succeeded", sum.Success, sum.Total)
		}
		if err := rotationService.Refresh(context.Background()); err != nil {
			log.Print("rotation refresh remains pending")
		}
		if err := rotationService.RefreshDaily(context.Background()); err != nil {
			log.Print("daily rotation refresh remains pending")
		}
		return
	}

	if *onceSW {
		industrySum := swSyncer.RunIndustry(context.Background())
		out, _ := json.MarshalIndent(industrySum, "", "  ")
		fmt.Println(string(out))
		if industrySum.Message != "ok" {
			log.Fatalf("one-shot sw industry sync failed: %s", industrySum.Message)
		}
		dailySum := swSyncer.RunDaily(context.Background(), nil)
		out, _ = json.MarshalIndent(dailySum, "", "  ")
		fmt.Println(string(out))
		if dailySum.Success != dailySum.Total {
			log.Fatalf("one-shot sw index daily sync incomplete: %d/%d succeeded", dailySum.Success, dailySum.Total)
		}
		return
	}

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	svcCtx := svc.NewServiceContext(c, syncer, swSyncer, signalComputer, st, st)
	handler.RegisterHandlers(server, svcCtx)
	handler.RegisterCatalog(server, svcCtx, st)
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/rotation/backtest", Handler: svcCtx.ApiKeyAuth(rotationService.Handler().ServeHTTP)})
	server.AddRoutes(rotationService.DailyRoutes(svcCtx.ApiKeyAuth))
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/rotation/backtest/range", Handler: svcCtx.ApiKeyAuth(rotationService.RangeHandler().ServeHTTP)})
	server.AddRoutes(rotationService.CaptureRoutes(svcCtx.ApiKeyAuth))
	runStore := syncrun.NewStore(db)
	server.AddRoutes(runStore.Routes(svcCtx.ApiKeyAuth))
	workerCtx, stopWorker := context.WithCancel(context.Background())
	referenceDone := make(chan struct{})
	go func() { defer close(referenceDone); rotationService.ServeReferences(workerCtx) }()
	defer func() { stopWorker(); <-referenceDone }()
	captureDone := make(chan struct{})
	go func() { defer close(captureDone); rotationService.ServeCaptures(workerCtx) }()
	defer func() { stopWorker(); <-captureDone }()
	rotationDone := make(chan struct{})
	go func() { defer close(rotationDone); rotationService.Serve(workerCtx) }()
	defer func() { stopWorker(); <-rotationDone }()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		(&syncrun.Worker{Store: runStore, Source: &syncrun.TushareSource{Client: tushareClient}}).Serve(workerCtx)
	}()
	proc.AddShutdownListener(stopWorker)
	defer func() { stopWorker(); <-workerDone }()

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
