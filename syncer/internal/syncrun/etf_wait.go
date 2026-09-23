package syncrun

import (
	"context"
	"syncer/internal/core"
	"time"
)

// Run preserves the old wait-for-result interface. The accepted batch belongs
// to the service worker, not to the request context that merely waits for it.
func (s *ETFService) Run(ctx context.Context, codes []string) core.Summary {
	b, _, err := s.Submit(ctx, codes)
	if err != nil {
		return core.Summary{Total: max(1, len(codes)), Results: []core.Result{{Message: "ETF 同步接受情况未确认，请查询管理记录。"}}}
	}
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		if b.State != "queued" && b.State != "running" {
			return etfSummary(b)
		}
		select {
		case <-ctx.Done():
			return core.Summary{Total: b.Total, Results: []core.Result{{Message: "等待已结束，已接受的后台任务继续执行。"}}}
		case <-timer.C:
		}
		next, err := s.Get(ctx, b.ID)
		if err != nil {
			return core.Summary{Total: b.Total, Results: []core.Result{{Message: "执行结果暂不可读，请查询管理记录；任务未被取消。"}}}
		}
		b = next
	}
}
func etfSummary(b ETFBatch) core.Summary {
	sum := core.Summary{Total: b.Total, Success: b.Success, Results: []core.Result{}}
	for _, item := range b.Items {
		message := item.Message
		if item.State == "succeeded" && item.DailyStart > item.EndDate && item.AdjStart > item.EndDate {
			message = "已是最新"
		}
		daily, adj := int(item.DailyRows), int(item.AdjRows)
		sum.Results = append(sum.Results, core.Result{TsCode: item.Code, StartDate: item.StartDate, EndDate: item.EndDate, Fetched: daily + adj, Upserted: daily + adj, DailyFetched: daily, DailyUpserted: daily, AdjFetched: adj, AdjUpserted: adj, Message: message, Successful: item.State == "succeeded"})
	}
	return sum
}
