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
	// A source revision invalidates recorded close groups, including dates
	// preceding the latest stored bar. Reference/capture-only archive entries
	// also need their first close once historical inputs have been repaired.
	// Raw history alone must not manufacture a daily archive.
	rows, err := s.DB.QueryContext(ctx, `SELECT archived.trade_date FROM `+archivedDates+`
 JOIN rotation_result r ON r.id=1
 LEFT JOIN rotation_daily d ON d.trade_date=archived.trade_date AND d.basis='close'
 WHERE archived.trade_date<=? AND (d.trade_date IS NULL OR d.revision<>r.revision OR d.status IN ('failed','computing'))
 UNION SELECT ? ORDER BY trade_date`, end, latest.String)
	if err != nil {
		return err
	}
	var dates []string
	for rows.Next() {
		var date string
		if err = rows.Scan(&date); err != nil {
			break
		}
		dates = append(dates, date)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, date := range dates {
		if err := s.PublishClose(ctx, date); err != nil {
			return err
		}
	}
	return nil
}
