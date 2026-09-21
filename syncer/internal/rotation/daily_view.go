package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"syncer/internal/core"
)

// Select both bases, progress and revision in one read snapshot. No calendar,
// quote or publication operation is allowed on this browser read path.
func (s *Service) dailyView(ctx context.Context, requested string) (DailyView, error) {
	now := s.now().In(shanghai)
	today := now.Format("20060102")
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return DailyView{}, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM rotation_result WHERE id=1").Scan(&revision); err != nil {
		return DailyView{}, err
	}
	var open bool
	calErr := tx.QueryRowContext(ctx, "SELECT is_open FROM rotation_calendar WHERE cal_date=?", today).Scan(&open)
	if calErr != nil && calErr != sql.ErrNoRows {
		return DailyView{}, calErr
	}
	calendarStatus := "ready"
	current := ""
	if calErr == nil {
		current, err = cachedTradingDay(ctx, tx, today, today)
		if err != nil {
			return DailyView{}, err
		}
	}
	if calErr != nil || current == "" {
		calendarStatus = "unavailable"
	}
	if requested != "" {
		v, err := readDailyDate(ctx, tx, requested, revision)
		if err != nil {
			return v, err
		}
		v.RequestedDate = requested
		v.SelectionMode = "manual"
		v.CurrentDate = today
		v.CurrentTradingDate = current
		v.CalendarStatus = calendarStatus
		return v, tx.Commit()
	}
	if calendarStatus != "ready" {
		v, e := readDailyDate(ctx, tx, "", revision)
		if e != nil {
			return v, e
		}
		v.SelectionMode = "default"
		v.CurrentDate = today
		v.Status = "calendar_unavailable"
		v.CalendarStatus = calendarStatus
		v.Message = "交易日历尚未缓存完整，无法确认默认展示日期"
		return v, tx.Commit()
	}
	desired := current
	var pending *DailyProgress
	fallback, reason := false, ""
	target, _ := captureTarget(today)
	if open {
		currentView, err := readDailyDate(ctx, tx, today, revision)
		if err != nil {
			return DailyView{}, err
		}
		if now.Before(target) || (currentView.Reference == nil && currentView.Close == nil) {
			prior, err := cachedTradingDay(ctx, tx, now.AddDate(0, 0, -1).Format("20060102"), today)
			if err != nil {
				return DailyView{}, err
			}
			if prior == "" {
				calendarStatus = "unavailable"
			}
			desired = prior
			fallback = true
			reason = "今日参考尚未发布，展示上一交易日已发布数据"
			if now.Before(target) {
				reason = "尚未到今日 14:45，展示上一交易日已发布数据"
				currentView.ReferenceState.Status = "waiting"
				currentView.ReferenceState.Message = "尚未到计划采集时点"
			} else if currentView.ReferenceState.Status == "missing" {
				reason = "今日 14:45 参考缺失，展示上一交易日已发布数据"
			}
			pending = &DailyProgress{TradeDate: today, Reference: currentView.ReferenceState, Close: currentView.CloseState}
		}
	} else {
		var available sql.NullString
		if err = tx.QueryRowContext(ctx, "SELECT MAX(trade_date) FROM rotation_daily WHERE trade_date<=? AND payload IS NOT NULL", current).Scan(&available); err != nil {
			return DailyView{}, err
		}
		if available.Valid {
			desired = available.String
		}
		fallback = true
		reason = "非交易日，展示最近交易日可用的已发布数据"
	}
	v, err := readDailyDate(ctx, tx, desired, revision)
	if err != nil {
		return v, err
	}
	v.SelectionMode = "default"
	v.CurrentDate = today
	v.CurrentTradingDate = current
	v.CalendarStatus = calendarStatus
	v.Fallback = fallback
	v.FallbackReason = reason
	v.Pending = pending
	if calendarStatus != "ready" {
		v.Status = "calendar_unavailable"
		v.Message = "交易日历尚未缓存完整，无法确认上一交易日"
		v.FallbackReason = v.Message
	} else if fallback && v.Close == nil && v.Reference == nil {
		v.Status = "unavailable"
		v.Message = "上一交易日暂无可用的已发布数据"
	}
	return v, tx.Commit()
}

// The entire interval to today must be cached; a missing holiday entry must not
// be silently interpreted as a closed day when finding the prior session.
func cachedTradingDay(ctx context.Context, tx *sql.Tx, through, today string) (string, error) {
	var latest sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT MAX(cal_date) FROM rotation_calendar WHERE is_open=1 AND cal_date<=?", through).Scan(&latest); err != nil {
		return "", err
	}
	if !latest.Valid {
		return "", nil
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM rotation_calendar WHERE cal_date BETWEEN ? AND ?", latest.String, today).Scan(&count); err != nil {
		return "", err
	}
	if count != int(day(today).Sub(day(latest.String)).Hours()/24)+1 {
		return "", nil
	}
	return latest.String, nil
}

func readDailyDate(ctx context.Context, tx *sql.Tx, date string, revision int64) (DailyView, error) {
	v := DailyView{TradeDate: date, Status: "unavailable", Message: "该交易日尚无已发布数据", ReferenceStatus: "missing", ReferenceState: DailyStage{Status: "missing", Message: "14:45 参考未留存", Missing: []DailyMissing{}}, CloseState: DailyStage{Status: "unavailable", Message: "收盘数据尚未发布", Missing: []DailyMissing{}}}
	for _, code := range core.RotationCodes {
		v.ReferenceState.Missing = append(v.ReferenceState.Missing, DailyMissing{Code: code, Reason: "原 14:45 数据未留存"})
		v.CloseState.Missing = append(v.CloseState.Missing, DailyMissing{Code: code, Reason: "收盘数据尚未发布"})
	}
	rows, err := tx.QueryContext(ctx, `SELECT basis,status,available,message,payload,revision,DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ'),missing FROM rotation_daily WHERE trade_date=?`, date)
	if err != nil {
		return v, err
	}
	referenceRecord := false
	for rows.Next() {
		var basis string
		var state DailyStage
		var data, missing []byte
		var rev int64
		if err = rows.Scan(&basis, &state.Status, &state.Available, &state.Message, &data, &rev, &state.UpdatedAt, &missing); err != nil {
			break
		}
		state.Missing = []DailyMissing{}
		if len(missing) > 0 {
			if err = json.Unmarshal(missing, &state.Missing); err != nil {
				break
			}
		}
		var result *DailyResult
		if len(data) > 0 {
			if err = json.Unmarshal(data, &result); err != nil {
				break
			}
			if result == nil || result.TradeDate != date || result.Basis != basis {
				err = fmt.Errorf("invalid daily publication identity")
				break
			}
		}
		switch basis {
		case "close":
			if state.Status == "ready" {
				state.Message = "四标的收盘数据已发布"
			}
			if rev != revision {
				state.Status = "updating"
				state.Message = "收盘数据正在更新，保留已发布完整结果"
			}
			v.Close = result
			v.CloseState = state
		case "reference_1445":
			referenceRecord = true
			v.Reference = result
			v.ReferenceState = state
		}
	}
	if rows.Err() != nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return v, err
	}
	if !referenceRecord {
		var state, stage, message, updated string
		var available int
		err = tx.QueryRowContext(ctx, `SELECT state,stage,available,message,DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ') FROM rotation_capture_run WHERE trade_date=?`, date).Scan(&state, &stage, &available, &message, &updated)
		if err != nil && err != sql.ErrNoRows {
			return v, err
		}
		if err == nil {
			status := "pending"
			if state == "partial" || state == "missing" {
				status = "missing"
			}
			v.ReferenceState = DailyStage{Status: status, Available: available, Message: message, UpdatedAt: updated, Missing: []DailyMissing{}}
			items, e := tx.QueryContext(ctx, "SELECT ts_code,reason FROM rotation_reference_input WHERE trade_date=? AND payload IS NULL ORDER BY ts_code", date)
			if e != nil {
				return v, e
			}
			for items.Next() {
				var item DailyMissing
				if e = items.Scan(&item.Code, &item.Reason); e != nil {
					break
				}
				v.ReferenceState.Missing = append(v.ReferenceState.Missing, item)
			}
			if items.Err() != nil {
				e = items.Err()
			}
			items.Close()
			if e != nil {
				return v, e
			}
		}
	}
	v.ReferenceStatus = v.ReferenceState.Status
	v.Available = v.CloseState.Available
	switch {
	case v.Close != nil:
		v.Status = v.CloseState.Status
		v.Message = v.CloseState.Message
		if v.Status == "ready" {
			if v.Reference != nil {
				v.Message = "同日 14:45 参考与收盘数据均已发布"
			} else {
				v.Message = "收盘数据已发布；14:45 参考缺失，对比不可用"
			}
		}
	case v.Reference != nil:
		v.Status = "ready"
		v.Message = "固定 14:45 参考已发布；" + v.CloseState.Message
	case v.CloseState.Status != "unavailable":
		v.Status = v.CloseState.Status
		v.Message = v.CloseState.Message
	case v.ReferenceState.Status == "pending" || v.ReferenceState.Status == "failed":
		v.Status = v.ReferenceState.Status
		v.Message = v.ReferenceState.Message
	}
	v.PriceSlippage = compareDailyPrices(v.Reference, v.Close)
	return v, nil
}
func compareDailyPrices(reference, close *DailyResult) []PriceSlippage {
	prices := func(result *DailyResult) map[string]*float64 {
		out := map[string]*float64{}
		if result != nil {
			for _, card := range result.Cards {
				out[card.Code] = card.Price
			}
		}
		return out
	}
	refs, closes := prices(reference), prices(close)
	out := make([]PriceSlippage, 0, 4)
	for _, code := range core.RotationCodes {
		item := PriceSlippage{Code: code, Reason: "同日参考或收盘价格缺失／无效"}
		ref, end := refs[code], closes[code]
		if reference != nil && close != nil && reference.TradeDate == close.TradeDate && ref != nil && end != nil && *ref > 0 && *end > 0 && finiteNumber(*ref) != nil && finiteNumber(*end) != nil {
			item.Bps = finiteNumber((*end - *ref) / *ref * 10000)
			if item.Bps != nil {
				item.Reason = ""
			}
		}
		out = append(out, item)
	}
	return out
}
