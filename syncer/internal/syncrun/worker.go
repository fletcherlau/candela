package syncrun

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"
)

type Worker struct {
	Store  *Store
	Source Source
}

// Serve is service-scoped, never attached to a browser request. The ticker only
// drains accepted work; daily business scheduling stays with external cron.
func (w *Worker) Serve(ctx context.Context) {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		r, err := w.Store.Claim(ctx)
		if err == nil {
			w.Execute(ctx, r)
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			log.Printf("index queue unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (w *Worker) Execute(ctx context.Context, r Run) {
	task, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-task.Done():
				return
			case <-ticker.C:
				beat, stop := context.WithTimeout(task, 5*time.Second)
				err := w.Store.Heartbeat(beat, r)
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	err := w.execute(task, r)
	cancel()
	<-done
	code, message := "", "同步完成。"
	if err != nil {
		code, message = "storage_error", "任务执行失败，已提交分段保留，请检查数据服务。"
		var sourceErr *SourceError
		if errors.As(err, &sourceErr) {
			code, message = sourceErr.Code, sourceErr.Message
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrOwnership) {
			code, message = "interrupted", "执行中断或执行权已过期，已提交分段保留。"
		}
	}
	// Graceful shutdown may record a failure; a crash is identified by lease expiry.
	finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := w.Store.Finish(finish, r, code, message); err != nil && !errors.Is(err, ErrOwnership) {
		log.Printf("index task terminal state could not be saved: %s", r.ID)
	}
}
func (w *Worker) execute(ctx context.Context, r Run) error {
	start, evidence := r.StartDate, "增量边界在提交时冻结；重取最后一个已存交易日。"
	if start == HistoryFloor {
		var err error
		start, evidence, err = w.Source.Earliest(ctx, r.EndDate)
		if err != nil {
			return err
		}
	}
	if _, err := time.Parse("20060102", start); err != nil || start < r.StartDate || start > r.EndDate {
		return &SourceError{"source_invalid", "数据源历史起点无效。"}
	}
	total := int(date(r.EndDate).Sub(date(start)).Hours()/24)/WindowDays + 1
	if err := w.Store.Plan(ctx, r, start, evidence, total); err != nil {
		return err
	}
	for from := date(start); !from.After(date(r.EndDate)); {
		to := from.AddDate(0, 0, WindowDays-1)
		if to.After(date(r.EndDate)) {
			to = date(r.EndDate)
		}
		bars, err := w.Source.Window(ctx, from.Format("20060102"), to.Format("20060102"))
		if err != nil {
			return err
		}
		if err = w.Store.CommitWindow(ctx, r, from.Format("20060102"), to.Format("20060102"), bars); err != nil {
			return err
		}
		from = to.AddDate(0, 0, 1)
	}
	return nil
}
