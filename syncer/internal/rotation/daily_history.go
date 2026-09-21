package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type DailyDate struct {
	TradeDate string     `json:"tradeDate"`
	Reference DailyStage `json:"reference"`
	Close     DailyStage `json:"close"`
}
type DailyDates struct {
	Status       string      `json:"status"`
	CurrentDate  string      `json:"currentDate"`
	EarliestDate string      `json:"earliestDate"`
	LatestDate   string      `json:"latestDate"`
	Dates        []DailyDate `json:"dates"`
	NextBefore   string      `json:"nextBefore"`
}

// The archive is the union of actual publications and capture attempts, not
// dates inferred from calendar or backtest history. A date cursor stays stable
// if newer records arrive while someone pages into the past.
const archivedDates = `(SELECT trade_date FROM rotation_daily UNION SELECT trade_date FROM rotation_capture_run) AS archived`

func (s *Service) dailyDates(ctx context.Context, before string, limit int) (DailyDates, error) {
	result := DailyDates{Status: "ready", CurrentDate: s.today(), Dates: []DailyDate{}}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var earliest, latest sql.NullString
	if err = tx.QueryRowContext(ctx, "SELECT MIN(trade_date),MAX(trade_date) FROM "+archivedDates+" WHERE trade_date<=?", result.CurrentDate).Scan(&earliest, &latest); err != nil {
		return result, err
	}
	result.EarliestDate, result.LatestDate = earliest.String, latest.String
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM rotation_result WHERE id=1").Scan(&revision); err != nil {
		return result, err
	}
	query := "SELECT trade_date FROM " + archivedDates + " WHERE trade_date<=?"
	args := []any{result.CurrentDate}
	if before != "" {
		query += " AND trade_date<?"
		args = append(args, before)
	}
	query += " ORDER BY trade_date DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	var dates []string
	for rows.Next() {
		var date string
		if err = rows.Scan(&date); err != nil {
			break
		}
		dates = append(dates, date)
	}
	scanErr := rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if scanErr != nil {
		return result, scanErr
	}
	if len(dates) > limit {
		dates = dates[:limit]
		result.NextBefore = dates[len(dates)-1]
	}
	for _, date := range dates {
		view, err := readDailyDate(ctx, tx, date, revision)
		if err != nil {
			return result, err
		}
		result.Dates = append(result.Dates, DailyDate{TradeDate: date, Reference: view.ReferenceState, Close: view.CloseState})
	}
	return result, tx.Commit()
}

func (s *Service) DailyDatesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "仅支持读取", 405)
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, "查询参数无效", 400)
			return
		}
		for key, values := range query {
			if (key != "before" && key != "limit") || len(values) != 1 || values[0] == "" {
				http.Error(w, "查询参数无效", 400)
				return
			}
		}
		before := query.Get("before")
		if before != "" {
			if _, err := time.Parse("20060102", before); err != nil || before > s.today() {
				http.Error(w, "交易日无效", 400)
				return
			}
		}
		limit := 30
		if raw := query.Get("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > 100 {
				http.Error(w, "分页数量无效", 400)
				return
			}
		}
		result, err := s.dailyDates(r.Context(), before, limit)
		if err != nil {
			http.Error(w, "每日档案暂不可用", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(result)
		}
	})
}
