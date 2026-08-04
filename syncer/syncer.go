// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

	"syncer/internal/config"
	"syncer/internal/handler"
	"syncer/internal/schema"
	"syncer/internal/svc"

	_ "github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

var (
	configFile = flag.String("f", "etc/syncer-api.yaml", "the config file")
	once       = flag.Bool("once", false, "run a one-shot sync and exit (debug/recovery channel)")
)

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c, conf.UseEnv())

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

	if *once {
		// 同步逻辑在后续 ticket 接入；本期仅打通 one-shot 入口。
		fmt.Println("one-shot mode: schema ensured, no sync logic yet")
		return
	}

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	svcCtx := svc.NewServiceContext(c)
	handler.RegisterHandlers(server, svcCtx)

	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	server.Start()
}
