package schema

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DDL in MySQL commits implicitly. Each statement is replayable, and the version
// is recorded only after all its statements succeed. A connection-scoped lock
// serializes startup migrations across syncer replicas.
var migrations = [][]string{{
	`CREATE TABLE IF NOT EXISTS index_series (
 ts_code VARCHAR(20) PRIMARY KEY, name VARCHAR(100) NOT NULL,
 fence BIGINT NOT NULL DEFAULT 0, active_run VARCHAR(32) NULL
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`INSERT IGNORE INTO index_series(ts_code,name) VALUES ('000985.CSI','中证全指')`,
	`CREATE TABLE IF NOT EXISTS index_daily (
 ts_code VARCHAR(20) NOT NULL, trade_date CHAR(8) NOT NULL,
 open DECIMAL(16,4) NOT NULL, high DECIMAL(16,4) NOT NULL, low DECIMAL(16,4) NOT NULL,
 close DECIMAL(16,4) NOT NULL, pre_close DECIMAL(16,4) NOT NULL, change_amt DECIMAL(16,4) NOT NULL,
 pct_chg DECIMAL(16,4) NOT NULL, vol DECIMAL(24,4) NOT NULL, amount DECIMAL(24,4) NOT NULL,
 PRIMARY KEY(ts_code,trade_date), FOREIGN KEY(ts_code) REFERENCES index_series(ts_code)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS sync_run (
 sequence BIGINT NOT NULL AUTO_INCREMENT UNIQUE,
 id VARCHAR(32) PRIMARY KEY, ts_code VARCHAR(20) NOT NULL, mode VARCHAR(16) NOT NULL,
 start_date CHAR(8) NOT NULL, end_date CHAR(8) NOT NULL, effective_start CHAR(8) NOT NULL DEFAULT '',
 state VARCHAR(16) NOT NULL DEFAULT 'queued', stage VARCHAR(32) NOT NULL DEFAULT 'queued',
 processed_rows BIGINT NOT NULL DEFAULT 0, completed_segments INT NOT NULL DEFAULT 0,
 total_segments INT NOT NULL DEFAULT 0, checkpoint CHAR(8) NOT NULL DEFAULT '',
 history_evidence TEXT NOT NULL, error_code VARCHAR(40) NOT NULL DEFAULT '', message VARCHAR(300) NOT NULL DEFAULT '',
 owner BIGINT NOT NULL DEFAULT 0, lease_until DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 KEY object_queue(ts_code,state,sequence), FOREIGN KEY(ts_code) REFERENCES index_series(ts_code)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}, {
	// Version 1 is already deployed. Widen only source-optional metrics, preserving
	// existing values; repeated ALTER is safe after interrupted migration startup.
	`ALTER TABLE index_daily
 MODIFY pre_close DECIMAL(16,4) NULL,
 MODIFY change_amt DECIMAL(16,4) NULL,
 MODIFY pct_chg DECIMAL(16,4) NULL,
 MODIFY vol DECIMAL(24,4) NULL,
 MODIFY amount DECIMAL(24,4) NULL`,
}}

func migrate(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK('candela_schema_migrations', 20)").Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return fmt.Errorf("schema migration lock unavailable")
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(release, "SELECT RELEASE_LOCK('candela_schema_migrations')")
	}()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migration (version INT PRIMARY KEY, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	for i, statements := range migrations {
		var applied int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migration WHERE version=?", i+1).Scan(&applied); err != nil {
			return err
		}
		if applied != 0 {
			continue
		}
		for _, statement := range statements {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migration %d: %w", i+1, err)
			}
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migration(version) VALUES (?)", i+1); err != nil {
			return err
		}
	}
	return nil
}
