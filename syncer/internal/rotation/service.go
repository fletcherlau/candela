// Package rotation publishes a complete historical backtest independently of readers.
package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"syncer/internal/core"
	"syncer/internal/syncrun"
	"time"
)

type CalendarSource interface {
	Calendar(context.Context, string, string) (map[string]bool, error)
}
type Service struct {
	ETFSync        *syncrun.ETFService
	Realtime       core.RealtimeSource
	DB             *sql.DB
	Calendar       CalendarSource
	Now            func() time.Time
	QuantileWindow int
}
type View struct {
	Status    string               `json:"status"`
	Message   string               `json:"message"`
	UpdatedAt string               `json:"updatedAt"`
	Result    *core.BacktestResult `json:"result"`
}

func relevant(code string) bool {
	for _, c := range core.RotationCodes {
		if c == code {
			return true
		}
	}
	return false
}
func horizon() string { return syncrun.Cutoff(time.Now()) }

// Daily/range reads and continuous calculation share the service clock. Keep
// the established 18:00 backtest cutoff, distinct from daily close eligibility.
func (s *Service) horizon() string { return syncrun.Cutoff(s.now()) }

func day(s string) time.Time { t, _ := time.Parse("20060102", s); return t }
func (s *Service) lock(ctx context.Context, wait int) (*sql.Conn, error) {
	c, e := s.DB.Conn(ctx)
	if e != nil {
		return nil, e
	}
	var ok int
	e = c.QueryRowContext(ctx, "SELECT GET_LOCK('candela_etf_publication', ?)", wait).Scan(&ok)
	if e != nil || ok != 1 {
		c.Close()
		return nil, fmt.Errorf("同步或计算正在进行")
	}
	return c, nil
}
func unlock(c *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.ExecContext(ctx, "SELECT RELEASE_LOCK('candela_etf_publication')")
	c.Close()
}

func (s *Service) BeginSync(ctx context.Context, codes []string) (func(core.Summary), error) {
	active := false
	for _, c := range codes {
		active = active || relevant(c)
	}
	if !active {
		return nil, nil
	}
	conn, err := s.lock(ctx, 60)
	if err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, "UPDATE rotation_result SET revision=revision+1,status='syncing',message='行情同步中，保留上一套完整结果' WHERE id=1"); err != nil {
		unlock(conn)
		return nil, err
	}
	return func(sum core.Summary) {
		defer unlock(conn)
		finish, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tx, e := conn.BeginTx(finish, nil)
		if e != nil {
			return
		}
		defer tx.Rollback()
		failed := false
		found := map[string]bool{}
		for _, r := range sum.Results {
			if relevant(r.TsCode) {
				found[r.TsCode] = true
				if !r.Succeeded() {
					failed = true
					continue
				}
				if _, e = tx.ExecContext(finish, "INSERT INTO rotation_coverage(ts_code,through_date) VALUES (?,?) ON DUPLICATE KEY UPDATE through_date=GREATEST(through_date,VALUES(through_date))", r.TsCode, r.EndDate); e != nil {
					return
				}
			}
		}
		for _, c := range codes {
			if relevant(c) && !found[c] {
				failed = true
			}
		}
		status, message := "pending", "行情已同步，回测正在更新"
		if failed {
			status, message = "failed", "行情同步未完成，保留上一套结果；待下次同步成功后重算"
		}
		if _, e = tx.ExecContext(finish, "UPDATE rotation_result SET revision=revision+1,status=?,message=? WHERE id=1", status, message); e != nil {
			return
		}
		tx.Commit()
	}, nil
}
func (s *Service) Serve(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		s.RefreshDaily(ctx)
		s.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) Refresh(ctx context.Context) error {
	return s.refreshRecovery(ctx, nil)
}

func (s *Service) refreshRecovery(ctx context.Context, recovery *RecoveryRun) error {
	conn, err := s.lock(ctx, 0)
	if err != nil {
		return err
	}
	defer unlock(conn)
	var status string
	var rev int64
	if err = conn.QueryRowContext(ctx, "SELECT status,revision FROM rotation_result WHERE id=1").Scan(&status, &rev); err != nil {
		return err
	}
	block, err := etfPublicationBlock(ctx, conn)
	if err != nil {
		return err
	}
	if block != "" {
		message := "ETF 同步执行中，保留上一套完整结果"
		if block == "failed" {
			message = "ETF 同步未完成，等待恢复；已发布结果保留"
		}
		err = updateBacktestPublication(ctx, conn, recovery, "UPDATE rotation_result SET status=?,message=? WHERE id=1 AND revision=?", block, message, rev)
		return err
	}
	if status == "syncing" { // The named lock was released by an interrupted synchronizer.
		err = updateBacktestPublication(ctx, conn, recovery, "UPDATE rotation_result SET status='failed',message='上次行情同步中断，等待重新同步；旧结果保留' WHERE id=1")
		return err
	}
	if status != "pending" && status != "computing" && !(recovery != nil && status == "failed") {
		return nil
	}
	err = updateBacktestPublication(ctx, conn, recovery, "UPDATE rotation_result SET status='computing',message='正在计算完整历史，保留上一套结果' WHERE id=1 AND revision=?", rev)
	if err != nil {
		return err
	}
	result, warning, err := s.calculate(ctx)
	if err != nil {
		// Errors produced by calculate describe data gaps, never upstream credentials.
		e := updateBacktestPublication(ctx, conn, recovery, "UPDATE rotation_result SET status='failed',message=? WHERE id=1 AND revision=?", truncate(err.Error()), rev)
		return e
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	state := "ready"
	if warning != "" {
		state = "stale"
	}
	err = updateBacktestPublication(ctx, conn, recovery, "UPDATE rotation_result SET payload=?,status=?,message=?,updated_at=CURRENT_TIMESTAMP(6) WHERE id=1 AND revision=?", payload, state, warning, rev)
	return err
}

// Recovery uses the existing calculation and publication path, adding the same
// execution-right fence already applied to daily groups. It does not create a
// second backtest queue or permit a displaced recovery to publish or fail it.
func updateBacktestPublication(ctx context.Context, conn *sql.Conn, recovery *RecoveryRun, query string, args ...any) error {
	if recovery == nil {
		_, err := conn.ExecContext(ctx, query, args...)
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return commitRecoveryPublication(ctx, tx, recovery)
}

func truncate(s string) string {
	r := []rune(s)
	if len(r) > 280 {
		return string(r[:280])
	}
	return s
}
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v View
		var data []byte
		var updated time.Time
		err := s.DB.QueryRowContext(r.Context(), "SELECT status,message,payload,updated_at FROM rotation_result WHERE id=1").Scan(&v.Status, &v.Message, &data, &updated)
		if err != nil {
			http.Error(w, "回测服务暂不可用", 503)
			return
		}
		v.UpdatedAt = updated.UTC().Format(time.RFC3339)
		if len(data) > 0 && json.Unmarshal(data, &v.Result) != nil {
			http.Error(w, "回测结果暂不可用", 503)
			return
		}
		if v.Status == "ready" && v.Result != nil {
			cutoff := s.horizon()
			var expected sql.NullString
			if err := s.DB.QueryRowContext(r.Context(), "SELECT MAX(cal_date) FROM rotation_calendar WHERE is_open=1 AND cal_date<=?", cutoff).Scan(&expected); err != nil {
				http.Error(w, "交易日历暂不可用", 503)
				return
			}
			var coverage sql.NullString
			s.DB.QueryRowContext(r.Context(), "SELECT MAX(cal_date) FROM rotation_calendar").Scan(&coverage)
			if !coverage.Valid || coverage.String < cutoff {
				v.Status = "stale"
				v.Message = "交易日历尚未更新，显示最近完整结果"
			} else if expected.Valid && v.Result.End < expected.String {
				v.Status = "stale"
				v.Message = "行情尚未更新至最新交易日，显示最近完整结果"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(v)
	})
}

func (s *Service) calculate(ctx context.Context) (*core.BacktestResult, string, error) {
	// A repeatable-read transaction makes all four price/factor histories one input.
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, "", fmt.Errorf("读取行情失败")
	}
	defer tx.Rollback()
	panels := make([]map[string]core.DailyBarAdj, 4)
	from := ""
	cutoff := s.horizon()
	end := cutoff
	for i, code := range core.RotationCodes {
		var through string
		if err = tx.QueryRowContext(ctx, "SELECT through_date FROM rotation_coverage WHERE ts_code=?", code).Scan(&through); err != nil {
			return nil, "", fmt.Errorf("%s 尚无已验证的日线和因子同步记录，请完成四标的同步", code)
		}
		if through < end {
			end = through
		}
		rows, e := tx.QueryContext(ctx, `SELECT d.trade_date,d.open,d.high,d.low,d.close,
   (SELECT a.adj_factor FROM etf_adj_factor a WHERE a.ts_code=d.ts_code AND a.trade_date<=d.trade_date ORDER BY a.trade_date DESC LIMIT 1)
   FROM etf_daily d WHERE d.ts_code=? ORDER BY d.trade_date`, code)
		if e != nil {
			return nil, "", fmt.Errorf("读取 %s 历史失败", code)
		}
		panels[i] = map[string]core.DailyBarAdj{}
		first := ""
		for rows.Next() {
			var b core.DailyBarAdj
			var o, h, l, c, f sql.NullFloat64
			if e = rows.Scan(&b.TradeDate, &o, &h, &l, &c, &f); e != nil {
				break
			}
			if first == "" {
				first = b.TradeDate
			}
			b.Open = o.Float64 * f.Float64
			b.High = h.Float64 * f.Float64
			b.Low = l.Float64 * f.Float64
			b.Close = c.Float64 * f.Float64
			panels[i][b.TradeDate] = b
		}
		if rows.Err() != nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return nil, "", fmt.Errorf("读取 %s 历史失败", code)
		}
		if first == "" {
			return nil, "", fmt.Errorf("%s 尚无日线历史", code)
		}
		if first > from {
			from = first
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("读取一致行情失败")
	}
	if end < from {
		return nil, "", fmt.Errorf("尚无共同有效的同步范围")
	}
	// Cache the authoritative calendar, including closed dates; never infer holidays.
	if err = s.ensureCalendar(ctx, from, cutoff); err != nil {
		return nil, "", err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT cal_date FROM rotation_calendar WHERE cal_date>=? AND cal_date<=? AND is_open=1 ORDER BY cal_date", from, end)
	if err != nil {
		return nil, "", fmt.Errorf("读取交易日历失败")
	}
	var dates []string
	for rows.Next() {
		var d string
		if err = rows.Scan(&d); err != nil {
			break
		}
		dates = append(dates, d)
	}
	if rows.Err() != nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, "", fmt.Errorf("读取交易日历失败")
	}
	aligned := make([][]core.DailyBarAdj, 4)
	warning := ""
	for _, d := range dates {
		valid := true
		for i := range panels {
			b, ok := panels[i][d]
			if !ok && verifiedSuspension(core.RotationCodes[i], d) && len(aligned[i]) > 0 {
				// Carry adjusted valuation only; do not fabricate or store a daily OHLC bar.
				b = core.DailyBarAdj{TradeDate: d, Close: aligned[i][len(aligned[i])-1].Close, Suspended: true}
				panels[i][d] = b
				continue
			}
			if !ok || b.Open <= 0 || b.Close <= 0 || b.Low <= 0 || b.High < b.Open || b.High < b.Close || b.Low > b.Open || b.Low > b.Close {
				warning = fmt.Sprintf("%s %s 日线或复权因子缺失／无效，结果仅到此前完整交易日", core.RotationCodes[i], d)
				valid = false
				break
			}
		}
		if !valid {
			break
		}
		for i := range panels {
			aligned[i] = append(aligned[i], panels[i][d])
		}
	}
	result, err := core.Backtest(aligned)
	if err != nil {
		if warning != "" {
			return nil, "", fmt.Errorf("%s；%s", warning, err)
		}
		return nil, "", err
	}
	if warning == "" && end < cutoff {
		warning = "同步覆盖尚未到当前截止日，显示已验证区间"
	}
	return result, warning, nil
}
func (s *Service) ensureCalendar(ctx context.Context, from, to string) error {
	for start := day(from); !start.After(day(to)); {
		end := start.AddDate(1, 0, -1)
		if end.After(day(to)) {
			end = day(to)
		}
		a, b := start.Format("20060102"), end.Format("20060102")
		var count int
		if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM rotation_calendar WHERE cal_date BETWEEN ? AND ?", a, b).Scan(&count); err != nil {
			return fmt.Errorf("读取交易日历失败")
		}
		if count != int(end.Sub(start).Hours()/24)+1 {
			if s.Calendar == nil {
				return fmt.Errorf("交易日历尚未接入")
			}
			days, err := s.Calendar.Calendar(ctx, a, b)
			if err != nil {
				return fmt.Errorf("交易日历源不可用，保留原结果")
			}
			for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
				if _, ok := days[d.Format("20060102")]; !ok {
					return fmt.Errorf("交易日历返回不完整")
				}
			}
			tx, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("保存交易日历失败")
			}
			for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
				k := d.Format("20060102")
				if _, err = tx.ExecContext(ctx, "INSERT INTO rotation_calendar(cal_date,is_open) VALUES (?,?) ON DUPLICATE KEY UPDATE is_open=VALUES(is_open)", k, days[k]); err != nil {
					break
				}
			}
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("保存交易日历失败")
			}
			if tx.Commit() != nil {
				return fmt.Errorf("保存交易日历失败")
			}
		}
		start = end.AddDate(0, 0, 1)
	}
	return nil
}
