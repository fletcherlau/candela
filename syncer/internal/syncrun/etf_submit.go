package syncrun

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ETFRequest describes a new batch. Retry never accepts a replacement scope.
type ETFRequest struct {
	Codes     []string `json:"codes"`
	Mode      string   `json:"mode"`
	StartDate string   `json:"startDate"`
	EndDate   string   `json:"endDate"`
}

func (s *ETFService) Submit(ctx context.Context, codes []string) (ETFBatch, bool, error) {
	return s.SubmitRequest(ctx, ETFRequest{Codes: codes})
}
func (s *ETFService) SubmitRequest(ctx context.Context, input ETFRequest) (ETFBatch, bool, error) {
	return s.submit(ctx, input, "")
}
func (s *ETFService) Retry(ctx context.Context, id string) (ETFBatch, bool, error) {
	return s.submit(ctx, ETFRequest{}, id)
}
func (s *ETFService) submit(ctx context.Context, input ETFRequest, parent string) (ETFBatch, bool, error) {
	// Keep the established ETF / close-report cutoff: submission day in Shanghai.
	today := s.now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("20060102")
	mode, start, end := input.Mode, input.StartDate, input.EndDate
	if mode == "" {
		mode = "incremental"
	}
	switch mode {
	case "incremental":
		if start != "" || end != "" {
			return ETFBatch{}, false, &etfRequestError{400, "增量同步不接受指定日期，请选择历史重同步。"}
		}
		end = today
	case "historical":
		from, firstErr := time.Parse("20060102", start)
		_, lastErr := time.Parse("20060102", end)
		if firstErr != nil || lastErr != nil || from.Year() < 1 || start > end || end > today {
			return ETFBatch{}, false, &etfRequestError{400, "请选择有效的历史起止日期，结束日期不能晚于北京时间今天。"}
		}
	default:
		return ETFBatch{}, false, &etfRequestError{400, "同步方式无效。"}
	}
	codes := input.Codes
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ETFBatch{}, false, err
	}
	defer tx.Rollback()
	parents := map[string]ETFItem{}
	if parent != "" {
		var locked string
		if err = tx.QueryRowContext(ctx, "SELECT id FROM etf_sync_batch WHERE id=? FOR UPDATE", parent).Scan(&locked); err != nil {
			return ETFBatch{}, false, err
		}
		var prior string
		err = tx.QueryRowContext(ctx, "SELECT id FROM etf_sync_batch WHERE parent_id=?", parent).Scan(&prior)
		if err == nil {
			b, e := readETFBatch(ctx, tx, prior)
			return b, true, e
		}
		if err != sql.ErrNoRows {
			return ETFBatch{}, false, err
		}
		original, e := readETFBatch(ctx, tx, parent)
		if e != nil {
			return ETFBatch{}, false, e
		}
		if original.State == "queued" || original.State == "running" {
			return ETFBatch{}, false, &etfRequestError{409, "原批次仍在执行，请先等待结果。"}
		}
		mode, start, end = original.Mode, original.StartDate, original.EndDate
		for _, item := range original.Items {
			if item.State == "failed" {
				codes = append(codes, item.Code)
				parents[item.Code] = item
			}
		}
		if len(codes) == 0 {
			return ETFBatch{}, false, &etfRequestError{409, "没有失败对象需要重试。"}
		}
	} else if len(codes) == 0 {
		rows, e := tx.QueryContext(ctx, "SELECT ts_code FROM instrument WHERE sync_enabled=1 ORDER BY ts_code")
		if e != nil {
			return ETFBatch{}, false, e
		}
		for rows.Next() {
			var code string
			if err = rows.Scan(&code); err != nil {
				break
			}
			codes = append(codes, code)
		}
		scanErr := rows.Err()
		rows.Close()
		if err != nil {
			return ETFBatch{}, false, err
		}
		if scanErr != nil {
			return ETFBatch{}, false, scanErr
		}
	}
	if len(codes) == 0 || len(codes) > 500 {
		return ETFBatch{}, false, &etfRequestError{400, "请选择 1 至 500 只 ETF。"}
	}
	unique := map[string]bool{}
	for _, code := range codes {
		if !etfCode.MatchString(code) {
			return ETFBatch{}, false, &etfRequestError{400, "ETF 代码格式无效。"}
		}
		unique[code] = true
	}
	codes = nil
	for code := range unique {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		if _, err = tx.ExecContext(ctx, "INSERT INTO etf_sync_object(ts_code) VALUES (?) ON DUPLICATE KEY UPDATE ts_code=VALUES(ts_code)", code); err != nil {
			return ETFBatch{}, false, err
		}
		var fence int64
		if err = tx.QueryRowContext(ctx, "SELECT fence FROM etf_sync_object WHERE ts_code=? FOR UPDATE", code).Scan(&fence); err != nil {
			return ETFBatch{}, false, err
		}
	}
	// Preserve the existing incremental key so pre-upgrade active batches dedupe.
	identity := strings.Join(codes, ",") + "/" + end
	if mode == "historical" {
		identity += "/historical/" + start
	}
	digest := sha256.Sum256([]byte(identity))
	key := hex.EncodeToString(digest[:])
	if parent == "" {
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT b.id FROM etf_sync_batch b WHERE request_key=? AND parent_id IS NULL AND EXISTS (SELECT 1 FROM etf_sync_item i JOIN etf_sync_run r ON r.id=i.run_id WHERE i.batch_id=b.id AND r.state IN ('queued','running','cancelling')) ORDER BY created_at DESC LIMIT 1`, key).Scan(&existing)
		if err == nil {
			b, e := readETFBatch(ctx, tx, existing)
			return b, true, e
		}
		if err != sql.ErrNoRows {
			return ETFBatch{}, false, err
		}
	}
	id, err := newRunID()
	if err != nil {
		return ETFBatch{}, false, err
	}
	var parentValue any
	if parent != "" {
		parentValue = parent
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO etf_sync_batch(id,parent_id,request_key,mode,start_date,end_date,created_at) VALUES (?,?,?,?,?,?,UTC_TIMESTAMP(6))", id, parentValue, key, mode, start, end); err != nil {
		return ETFBatch{}, false, err
	}
	for _, code := range codes {
		runID, e := s.acceptETFRun(ctx, tx, code, mode, start, end, parents[code])
		if e != nil {
			return ETFBatch{}, false, e
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO etf_sync_item(batch_id,ts_code,run_id) VALUES (?,?,?)", id, code, runID); err != nil {
			return ETFBatch{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return ETFBatch{}, false, err
	}
	b, err := s.Get(ctx, id)
	return b, false, err
}
func (s *ETFService) acceptETFRun(ctx context.Context, tx *sql.Tx, code, mode, requestedStart, end string, parent ETFItem) (string, error) {
	p := parent.ETFProgress
	if parent.ID == "" {
		if mode == "incremental" {
			if _, err := time.Parse("20060102", s.DefaultStart); err != nil {
				return "", fmt.Errorf("invalid ETF history floor")
			}
		}
		p = ETFProgress{DailyStart: s.DefaultStart, AdjStart: s.DefaultStart, ChunkDays: s.ChunkDays}
		if p.ChunkDays < 1 || p.ChunkDays > 366 {
			p.ChunkDays = 366
		}
		if mode == "historical" {
			// Never consult the table maximum: this range intentionally revisits old rows.
			p.DailyStart, p.AdjStart = requestedStart, requestedStart
		} else {
			var daily, adj sql.NullString
			if err := tx.QueryRowContext(ctx, "SELECT MAX(trade_date) FROM etf_daily WHERE ts_code=? AND trade_date<=?", code, end).Scan(&daily); err != nil {
				return "", err
			}
			if err := tx.QueryRowContext(ctx, "SELECT MAX(trade_date) FROM etf_adj_factor WHERE ts_code=? AND trade_date<=?", code, end).Scan(&adj); err != nil {
				return "", err
			}
			if daily.Valid && adj.Valid {
				p.DailyStart = date(daily.String).AddDate(0, 0, 1).Format("20060102")
				p.AdjStart = date(adj.String).AddDate(0, 0, 1).Format("20060102")
			}
		}
	} else {
		p.ParentRun = parent.ID
	}
	start := p.DailyStart
	if p.AdjStart < start {
		start = p.AdjStart
	}
	total := segments(p.DailyStart, end, p.ChunkDays) + segments(p.AdjStart, end, p.ChunkDays)
	complete := 0
	if p.DailyCheckpoint != "" {
		complete += segments(p.DailyStart, p.DailyCheckpoint, p.ChunkDays)
	}
	if p.AdjCheckpoint != "" {
		complete += segments(p.AdjStart, p.AdjCheckpoint, p.ChunkDays)
	}
	checkpoint := ""
	if total == 0 {
		total = 1
		complete = 1
		checkpoint = end
	}
	if complete == total {
		checkpoint = end
	}
	id, err := newRunID()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO etf_sync_run(id,ts_code,mode,start_date,end_date,effective_start,history_evidence,total_segments,completed_segments,processed_rows,checkpoint,created_at,updated_at) VALUES (?,?,?,?,?,?,'日线和因子分别冻结范围与检查点；原范围恢复',?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id, code, mode, start, end, start, total, complete, p.DailyRows+p.AdjRows, checkpoint)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO etf_sync_progress(run_id,"+etfProgressColumns+") VALUES (?,?,?,?,?,?,?,?,?)", id, p.DailyStart, p.AdjStart, p.DailyCheckpoint, p.AdjCheckpoint, p.DailyRows, p.AdjRows, p.ChunkDays, p.ParentRun)
	if err != nil {
		return "", err
	}
	if rotationCode(code) {
		_, err = tx.ExecContext(ctx, "UPDATE rotation_result SET revision=revision+1,status='syncing',message='ETF 同步已接受，保留上一套完整结果' WHERE id=1")
	}
	return id, err
}
