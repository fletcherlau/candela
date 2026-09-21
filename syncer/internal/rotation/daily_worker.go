package rotation

import (
	"context"
	"database/sql"
	"time"
)

// RefreshDaily runs with the background publisher, never in a browser request.
// Official daily data is eligible only after that Shanghai trading day's close.
func (s *Service) RefreshDaily(ctx context.Context) error {
	now := s.now().In(time.FixedZone("Asia/Shanghai", 8*3600))
	// Cache today's authoritative calendar before the 14:45 external trigger.
	// This existing publisher is the maintenance path; reads remain source-free.
	today := now.Format("20060102")
	if err := s.ensureCalendar(ctx, today, today); err != nil {
		return err
	}
	if now.Hour() < 15 {
		now = now.AddDate(0, 0, -1)
	}
	end := now.Format("20060102")
	var latest sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT MAX(trade_date) FROM etf_daily WHERE ts_code IN ('510880.SH','518880.SH','159915.SZ','513100.SH') AND trade_date<=?`, end).Scan(&latest); err != nil {
		return err
	}
	if !latest.Valid {
		return nil
	}
	return s.PublishClose(ctx, latest.String)
}
