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
	"time"

	"syncer/internal/config"
	"syncer/internal/core"
	"syncer/internal/handler"
	"syncer/internal/schema"
	"syncer/internal/store"
	"syncer/internal/svc"
	"syncer/internal/tushare"

	_ "github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

var (
	configFile = flag.String("f", "etc/syncer-api.yaml", "the config file")
	once       = flag.Bool("once", false, "run a one-shot sync for all sync-enabled instruments and exit")
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

	source := tushare.NewClient(c.Tushare.BaseURL, c.Tushare.Token)
	throttler := tushare.NewThrottler(time.Duration(c.Sync.ThrottleMs) * time.Millisecond)
	syncer := core.NewSyncer(source, store.NewMySQLStore(db),
		c.Sync.ChunkDays, c.Sync.DefaultStartDate, throttler.Wait, nil)

	if *once {
		sum := syncer.Run(context.Background(), nil)
		out, _ := json.MarshalIndent(sum, "", "  ")
		fmt.Println(string(out))
		if sum.Success != sum.Total {
			log.Fatalf("one-shot sync incomplete: %d/%d succeeded", sum.Success, sum.Total)
		}
		return
	}

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	svcCtx := svc.NewServiceContext(c, syncer)
	handler.RegisterHandlers(server, svcCtx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
