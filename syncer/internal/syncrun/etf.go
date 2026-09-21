package syncrun

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"regexp"
	"syncer/internal/core"
	"time"
)

type ETFService struct {
	Store        *Store
	Source       core.QuoteSource
	DefaultStart string
	ChunkDays    int
	Now          func() time.Time
}

func NewETFStore(db *sql.DB) *Store {
	return &Store{db: db, objects: "etf_sync_object", runs: "etf_sync_run", events: "etf_sync_event"}
}
func (s *ETFService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func newRunID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var etfCode = regexp.MustCompile(`^[0-9]{6}\.(SH|SZ)$`)

type etfRequestError struct {
	status  int
	message string
}

func (e *etfRequestError) Error() string { return e.message }

type ETFProgress struct {
	DailyStart      string `json:"dailyStart"`
	AdjStart        string `json:"adjStart"`
	DailyCheckpoint string `json:"dailyCheckpoint"`
	AdjCheckpoint   string `json:"adjCheckpoint"`
	DailyRows       int64  `json:"dailyRows"`
	AdjRows         int64  `json:"adjRows"`
	ChunkDays       int    `json:"chunkDays"`
	ParentRun       string `json:"parentRun"`
}
type ETFItem struct {
	Run
	ETFProgress
}
type ETFEvent struct {
	Event
	Code string `json:"code"`
}
type ETFBatch struct {
	Mode      string     `json:"mode"`
	StartDate string     `json:"startDate"`
	Events    []ETFEvent `json:"events,omitempty"`
	ID        string     `json:"id"`
	ParentID  string     `json:"parentId"`
	EndDate   string     `json:"endDate"`
	CreatedAt string     `json:"createdAt"`
	State     string     `json:"state"`
	Total     int        `json:"total"`
	Success   int        `json:"success"`
	Failed    int        `json:"failed"`
	Cancelled int        `json:"cancelled"`
	Items     []ETFItem  `json:"items"`
}

const etfProgressColumns = "daily_start,adj_start,daily_checkpoint,adj_checkpoint,daily_rows,adj_rows,chunk_days,parent_run"

func progress(row scanner) (p ETFProgress, err error) {
	err = row.Scan(&p.DailyStart, &p.AdjStart, &p.DailyCheckpoint, &p.AdjCheckpoint, &p.DailyRows, &p.AdjRows, &p.ChunkDays, &p.ParentRun)
	return
}
func readETFBatch(ctx context.Context, tx *sql.Tx, id string) (b ETFBatch, err error) {
	var parent sql.NullString
	if err = tx.QueryRowContext(ctx, "SELECT id,parent_id,mode,start_date,end_date,DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%sZ') FROM etf_sync_batch WHERE id=?", id).Scan(&b.ID, &parent, &b.Mode, &b.StartDate, &b.EndDate, &b.CreatedAt); err != nil {
		return
	}
	b.ParentID = parent.String
	b.Items = []ETFItem{}
	// A single statement pairs each state with its committed checkpoints, even
	// when called by READ COMMITTED acceptance/retry transactions.
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+","+etfProgressColumns+" FROM etf_sync_run r JOIN etf_sync_progress p ON p.run_id=r.id WHERE r.id IN (SELECT run_id FROM etf_sync_item WHERE batch_id=?) ORDER BY r.ts_code", id)
	if err != nil {
		return b, err
	}
	active, running := false, false
	for rows.Next() {
		var p ETFProgress
		run, e := scan(rows, &p.DailyStart, &p.AdjStart, &p.DailyCheckpoint, &p.AdjCheckpoint, &p.DailyRows, &p.AdjRows, &p.ChunkDays, &p.ParentRun)
		if e != nil {
			rows.Close()
			return b, e
		}
		b.Items = append(b.Items, ETFItem{run, p})
		b.Total++
		switch run.State {
		case "succeeded":
			b.Success++
		case "failed":
			b.Failed++
		case "cancelled":
			b.Cancelled++
		default:
			active = true
			running = running || run.State != "queued"
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	b.setState(active, running)
	rows, err = tx.QueryContext(ctx, `SELECT i.ts_code,e.kind,DATE_FORMAT(e.created_at,'%Y-%m-%dT%H:%i:%sZ'),e.checkpoint,e.message FROM etf_sync_event e JOIN etf_sync_item i ON i.run_id=e.run_id WHERE i.batch_id=? ORDER BY e.sequence DESC LIMIT 200`, id)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var event ETFEvent
		if err = rows.Scan(&event.Code, &event.Kind, &event.At, &event.Checkpoint, &event.Message); err != nil {
			rows.Close()
			return b, err
		}
		b.Events = append(b.Events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return b, err
	}
	return b, nil
}

func (b *ETFBatch) setState(active, running bool) {
	b.State = "queued"
	switch {
	case active:
		if running || b.Success+b.Failed+b.Cancelled > 0 {
			b.State = "running"
		}
	case b.Cancelled > 0:
		b.State = "cancelled"
	case b.Failed > 0:
		b.State = "failed"
		if b.Success > 0 {
			b.State = "partial"
		}
	case b.Total > 0:
		b.State = "succeeded"
	}
}
func (s *ETFService) Get(ctx context.Context, id string) (ETFBatch, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ETFBatch{}, err
	}
	defer tx.Rollback()
	b, err := readETFBatch(ctx, tx, id)
	if err != nil {
		return b, err
	}
	return b, tx.Commit()
}
func (s *ETFService) List(ctx context.Context) ([]ETFBatch, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// List summaries are bounded independently of the number of ETF items.
	rows, err := tx.QueryContext(ctx, `SELECT b.id,COALESCE(b.parent_id,''),b.mode,b.start_date,b.end_date,
 DATE_FORMAT(b.created_at,'%Y-%m-%dT%H:%i:%sZ'),COUNT(r.id),
 COALESCE(SUM(r.state='succeeded'),0),COALESCE(SUM(r.state='failed'),0),
 COALESCE(SUM(r.state='cancelled'),0),COALESCE(SUM(r.state IN ('running','cancelling')),0)
 FROM (SELECT * FROM etf_sync_batch ORDER BY created_at DESC,id DESC LIMIT 50) b
 LEFT JOIN etf_sync_item i ON i.batch_id=b.id LEFT JOIN etf_sync_run r ON r.id=i.run_id
 GROUP BY b.id,b.parent_id,b.mode,b.start_date,b.end_date,b.created_at ORDER BY b.created_at DESC,b.id DESC`)
	if err != nil {
		return nil, err
	}
	out := []ETFBatch{}
	for rows.Next() {
		var b ETFBatch
		var running int
		if err = rows.Scan(&b.ID, &b.ParentID, &b.Mode, &b.StartDate, &b.EndDate, &b.CreatedAt, &b.Total, &b.Success, &b.Failed, &b.Cancelled, &running); err != nil {
			rows.Close()
			return nil, err
		}
		b.setState(b.Total > b.Success+b.Failed+b.Cancelled, running > 0)
		out = append(out, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func segments(from, to string, chunk int) int {
	if from > to {
		return 0
	}
	return int((date(to).Unix()-date(from).Unix())/86400)/chunk + 1
}
func rotationCode(code string) bool {
	for _, c := range core.RotationCodes {
		if c == code {
			return true
		}
	}
	return false
}
func updateETFPublication(ctx context.Context, tx *sql.Tx, id, code string) error {
	if !rotationCode(code) {
		return nil
	}
	var state, mode, end string
	if err := tx.QueryRowContext(ctx, "SELECT state,mode,end_date FROM etf_sync_run WHERE id=?", id).Scan(&state, &mode, &end); err != nil {
		return err
	}
	status, message := "failed", "ETF 同步未完成，保留上一套完整结果"
	if state == "succeeded" {
		// Re-querying a narrow historical window cannot establish full coverage
		// through its end. Preserve the coverage proved by incremental sync.
		if mode == "incremental" {
			if _, err := tx.ExecContext(ctx, "INSERT INTO rotation_coverage(ts_code,through_date) VALUES (?,?) ON DUPLICATE KEY UPDATE through_date=GREATEST(through_date,VALUES(through_date))", code, end); err != nil {
				return err
			}
		}
		status, message = "pending", "原始数据已更新，等待整组计算发布"
	}
	_, err := tx.ExecContext(ctx, "UPDATE rotation_result SET revision=revision+1,status=?,message=? WHERE id=1", status, message)
	return err
}
func (s *ETFService) Cancel(ctx context.Context, id string) (ETFBatch, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ETFBatch{}, err
	}
	defer tx.Rollback()
	var locked string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM etf_sync_batch WHERE id=? FOR UPDATE", id).Scan(&locked); err != nil {
		return ETFBatch{}, err
	}
	b, err := readETFBatch(ctx, tx, id)
	if err != nil {
		return b, err
	}
	for _, item := range b.Items {
		var fence int64
		if err = tx.QueryRowContext(ctx, "SELECT fence FROM etf_sync_object WHERE ts_code=? FOR UPDATE", item.Code).Scan(&fence); err != nil {
			return b, err
		}
		var state string
		if err = tx.QueryRowContext(ctx, "SELECT state FROM etf_sync_run WHERE id=? FOR UPDATE", item.ID).Scan(&state); err != nil {
			return b, err
		}
		switch state {
		case "queued":
			err = s.Store.markCancelled(ctx, tx, item.ID, item.Code)
		case "running":
			_, err = tx.ExecContext(ctx, "UPDATE etf_sync_run SET state='cancelling',message='正在取消；已提交步骤保留',updated_at=UTC_TIMESTAMP(6) WHERE id=?", item.ID)
			if err == nil {
				err = s.Store.recordEvent(ctx, tx, item.ID, "cancel_requested", "已请求取消，不再提交新的分段。")
			}
		}
		if err != nil {
			return b, err
		}
	}
	if err = tx.Commit(); err != nil {
		return b, err
	}
	return s.Get(ctx, id)
}
