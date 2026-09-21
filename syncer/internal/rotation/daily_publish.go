package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"syncer/internal/core"
	"time"
)

// PublishClose uses the existing source revision and publication lock. The
// locking revision read at commit prevents a superseded snapshot being exposed.
func (s *Service) PublishClose(ctx context.Context, date string) error {
	return s.publishCloseRecovery(ctx, date, nil)
}
func (s *Service) publishCloseRecovery(ctx context.Context, date string, recovery *RecoveryRun) (publishErr error) {
	if _, err := time.Parse("20060102", date); err != nil || date > s.today() {
		return fmt.Errorf("交易日无效")
	}
	conn, err := s.lock(ctx, 0)
	if err != nil {
		return err
	}
	defer unlock(conn)
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int64
	var syncState string
	if err = tx.QueryRowContext(ctx, "SELECT revision,status FROM rotation_result WHERE id=1").Scan(&revision, &syncState); err != nil {
		return err
	}
	block, err := etfPublicationBlock(ctx, tx)
	if err != nil {
		return err
	}
	if syncState == "syncing" && block == "" {
		return nil
	}
	defer func() {
		if publishErr == nil || errors.Is(publishErr, errRecoveryOwnership) {
			return
		}
		// Roll back the input snapshot before recording failure. Guard this write
		// too: a failed superseded calculation must not replace newer state.
		_ = tx.Rollback()
		failureTx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		defer failureTx.Rollback()
		_, err = failureTx.ExecContext(ctx, `INSERT INTO rotation_daily(trade_date,basis,revision,status,message)
 SELECT ?,'close',revision,'failed','收盘计算失败，等待后台重试；已发布完整结果保留' FROM rotation_result WHERE id=1 AND revision=?
 ON DUPLICATE KEY UPDATE revision=VALUES(revision),status=VALUES(status),message=VALUES(message),updated_at=CURRENT_TIMESTAMP(6)`, date, revision)
		if err == nil {
			_ = commitRecoveryPublication(ctx, failureTx, recovery)
		}
	}()
	var previous int64
	var previousStatus string
	err = tx.QueryRowContext(ctx, "SELECT revision,status FROM rotation_daily WHERE trade_date=? AND basis='close'", date).Scan(&previous, &previousStatus)
	if err == nil && previous == revision && previousStatus != "failed" {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err = s.ensureCalendar(ctx, date, date); err != nil {
		return err
	}
	var isOpen bool
	// Calendar may have been filled after the repeatable-read snapshot began.
	if err = s.DB.QueryRowContext(ctx, "SELECT is_open FROM rotation_calendar WHERE cal_date=?", date).Scan(&isOpen); err != nil {
		return err
	}
	if !isOpen {
		return fmt.Errorf("所选日期不是交易日")
	}
	panels, prices, reasons, err := s.closeInputs(ctx, tx, date)
	if err != nil {
		return err
	}
	available := len(prices)
	status, message := "pending", fmt.Sprintf("收盘数据已到齐 %d/4", available)
	missing := []DailyMissing{}
	for _, code := range core.RotationCodes {
		if _, present := prices[code]; !present {
			missing = append(missing, DailyMissing{Code: code, Reason: reasons[code]})
			message += "；" + code + "：" + reasons[code]
		}
	}
	missingJSON, err := json.Marshal(missing)
	if err != nil {
		return err
	}
	var payload []byte
	var published any
	if block != "" {
		message += "；同步尚未完整结束，整组结果暂不发布"
		if block == "failed" {
			status = "failed"
		}
	}
	if available == len(core.RotationCodes) && block == "" {
		result := s.dailyResult(date, panels, prices, reasons, CaptureParams{Version: indicatorVersion, QuantileWindow: s.quantileWindow()})
		payload, err = json.Marshal(result)
		if err != nil {
			return err
		}
		published = s.now().UTC()
		status, message = "ready", "四标的收盘数据已发布"
	}
	var current int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM rotation_result WHERE id=1 FOR UPDATE").Scan(&current); err != nil {
		return err
	}
	if current != revision {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO rotation_daily(trade_date,basis,revision,status,available,message,payload,published_at,missing)
 VALUES (?,'close',?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE revision=VALUES(revision),status=VALUES(status),available=VALUES(available),message=VALUES(message),
 missing=VALUES(missing),payload=COALESCE(VALUES(payload),payload),published_at=COALESCE(VALUES(published_at),published_at),updated_at=CURRENT_TIMESTAMP(6)`, date, revision, status, available, message, payload, published, missingJSON)
	if err != nil {
		return err
	}
	return commitRecoveryPublication(ctx, tx, recovery)
}

func (s *Service) dailyResult(date string, panels map[string][]core.DailyBarAdj, prices map[string]float64, reasons map[string]string, params CaptureParams) DailyResult {
	result := DailyResult{TradeDate: date, Basis: "close", Available: len(prices), Source: "Tushare 基金日线及复权因子", Version: params.Version, QuantileWindow: params.QuantileWindow, PublishedAt: s.now().UTC().Format(time.RFC3339Nano)}
	cards := core.ComputeRotationCards(panels, params.QuantileWindow)
	completeRank := true
	for _, card := range cards {
		if finiteNumber(card.Score) == nil {
			completeRank = false
		}
	}
	for i, card := range cards {
		p := prices[card.TsCode]
		item := DailyCard{Code: card.TsCode, Name: core.RotationNames[i], Price: &p, Score: finiteNumber(card.Score), Volatility: finiteNumber(card.YZVol), Quantile: finiteNumber(card.Quantile), Reasons: map[string]string{}}
		if completeRank {
			rank := card.Rank
			item.Rank = &rank
		} else {
			item.Reasons["rank"] = "四标的动量未全部可用"
		}
		if item.Quantile != nil {
			item.Weight = finiteNumber(card.Weight)
		}
		for key, value := range map[string]*float64{"score": item.Score, "volatility": item.Volatility, "quantile": item.Quantile, "weight": item.Weight} {
			if value == nil {
				item.Reasons[key] = "所需历史数据不足，无法计算"
				if reason := reasons[card.TsCode]; reason != "" {
					item.Reasons[key] = reason
				}
			}
		}
		result.Cards = append(result.Cards, item)
	}
	return result
}
