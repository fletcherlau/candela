package config

import "github.com/zeromicro/go-zero/rest"

type Config struct {
	rest.RestConf
	// MysqlDSN 经环境变量 MYSQL_DSN 注入（必填，缺失时启动即报错），例如：
	// root:pass@tcp(127.0.0.1:3306)/candela?charset=utf8mb4&parseTime=true&loc=Local
	MysqlDSN string
	Tushare  struct {
		// Token 经环境变量 TUSHARE_TOKEN 注入。
		Token   string `json:",optional"`
		BaseURL string `json:",default=https://api.tushare.pro"`
	}
	Sync struct {
		// ThrottleMs 任意两次 Tushare 调用的最小间隔（毫秒）。
		ThrottleMs int `json:",default=300"`
		// DefaultStartDate 无历史数据时全量回填的起始日期，YYYYMMDD。
		DefaultStartDate string `json:",default=20100101"`
		// ChunkDays 单次拉取的日期分片天数，规避 Tushare 单次 2000 行上限。
		ChunkDays int `json:",default=370"`
	}
	// ApiKey 触发接口鉴权密钥，经环境变量 SYNC_API_KEY 注入；为空则不鉴权（仅限本地调试）。
	ApiKey string `json:",optional"`
}
