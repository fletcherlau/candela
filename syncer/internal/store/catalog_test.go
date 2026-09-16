package store_test

import (
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"os"
	"syncer/internal/schema"
	"syncer/internal/store"
	"testing"
)

func TestCatalogReportsStoredCoverageAndEmptyDatasets(t *testing.T) {
	dsn := os.Getenv("CATALOG_TEST_DSN")
	if dsn == "" {
		t.Skip("CATALOG_TEST_DSN must point to an isolated test database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "candela_catalog_test" {
		t.Fatal("refusing non-test database")
	}
	ctx := context.Background()
	if err := schema.Ensure(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"index_daily", "instrument", "etf_daily", "etf_adj_factor", "sw_industry", "sw_index_daily", "sw_industry_member"} {
		if _, err := db.Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"INSERT INTO instrument(ts_code,name,sync_enabled) VALUES ('510300.SH','沪深300ETF',1),('159999.SZ','未同步ETF',0)",
		"INSERT INTO etf_daily(ts_code,trade_date) VALUES ('510300.SH','20240102'),('510300.SH','20240906')",
		"INSERT INTO etf_adj_factor(ts_code,trade_date,adj_factor) VALUES ('510300.SH','20240101',1),('510300.SH','20240909',1)",
		"INSERT INTO sw_industry(index_code,industry_name,level) VALUES ('801010.SI','农林牧渔','L1')",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	result := store.NewMySQLStore(db).Catalog(ctx)
	if len(result.Groups) != 3 {
		t.Fatalf("groups: %+v", result)
	}
	if result.Groups[0].Items[0].Status != "no_data" {
		t.Fatal("CSI must not pretend to have data")
	}
	etfs := result.Groups[1].Items
	var found bool
	for _, item := range etfs {
		if item.ID == "etf-daily:510300.SH" {
			found = true
			if item.Rows == nil || *item.Rows != 2 || item.StartDate != "20240102" || item.EndDate != "20240906" || item.Status != "available" {
				t.Fatalf("daily coverage: %+v", item)
			}
		}
		if item.Code == "159999.SZ" && (item.Status != "no_data" || item.SyncEnabled == nil || *item.SyncEnabled) {
			t.Fatalf("empty disabled ETF: %+v", item)
		}
		if item.ID == "etf-factor:510300.SH" && item.EndDate != "20240909" {
			t.Fatal("factor dates must remain independent")
		}
	}
	if !found {
		t.Fatal("missing stored ETF")
	}
	if _, err := db.Exec("DROP TABLE sw_industry_member"); err != nil {
		t.Fatal(err)
	}
	result = store.NewMySQLStore(db).Catalog(ctx)
	if result.Groups[2].Status != "error" || result.Groups[1].Status != "available" {
		t.Fatal("failed SW query must not look empty or hide ETF coverage")
	}
}
