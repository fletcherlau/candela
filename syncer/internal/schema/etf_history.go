package schema

import (
	"context"
	"database/sql"
	"fmt"
)

// Additive and replayable after MySQL's implicit DDL commit. Existing batches
// remain incremental and retain their per-object frozen phase starts.
func etfHistoryMigration(ctx context.Context, conn *sql.Conn) error {
	for _, column := range []struct {
		name, kind, definition, expectedDefault string
		length                                  int
	}{
		{"mode", "varchar", "VARCHAR(16) NOT NULL DEFAULT 'incremental'", "incremental", 16},
		{"start_date", "char", "CHAR(8) NOT NULL DEFAULT ''", "", 8},
	} {
		var kind, nullable, extra string
		var defaultValue sql.NullString
		var length int
		err := conn.QueryRowContext(ctx, `SELECT DATA_TYPE,IS_NULLABLE,EXTRA,CHARACTER_MAXIMUM_LENGTH,COLUMN_DEFAULT
 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='etf_sync_batch' AND COLUMN_NAME=?`, column.name).Scan(&kind, &nullable, &extra, &length, &defaultValue)
		if err == sql.ErrNoRows {
			if _, err = conn.ExecContext(ctx, "ALTER TABLE etf_sync_batch ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if kind != column.kind || nullable != "NO" || extra != "" || length != column.length || !defaultValue.Valid || defaultValue.String != column.expectedDefault {
			return fmt.Errorf("etf_sync_batch.%s has incompatible definition", column.name)
		}
	}
	return nil
}
