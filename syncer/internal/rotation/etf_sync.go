package rotation

import (
	"context"
	"database/sql"
)

type rotationRowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// New ETF execution is durable. A named publication lock being free no longer
// means its synchronizer died. A candidate may only publish after the current
// runs finish; revision guards reject source changes after this read snapshot.
func etfPublicationBlock(ctx context.Context, q rotationRowReader) (string, error) {
	var active int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM etf_sync_run WHERE ts_code IN ('510880.SH','518880.SH','159915.SZ','513100.SH') AND state IN ('queued','running','cancelling')`).Scan(&active); err != nil {
		return "", err
	}
	if active > 0 {
		return "syncing", nil
	}
	var failed int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM etf_sync_run r JOIN (SELECT ts_code,MAX(sequence) AS last_sequence FROM etf_sync_run WHERE ts_code IN ('510880.SH','518880.SH','159915.SZ','513100.SH') GROUP BY ts_code) latest ON r.sequence=latest.last_sequence WHERE r.state IN ('failed','cancelled')`).Scan(&failed); err != nil {
		return "", err
	}
	if failed > 0 {
		return "failed", nil
	}
	return "", nil
}
