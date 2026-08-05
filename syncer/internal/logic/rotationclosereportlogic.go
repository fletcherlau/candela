package logic

import (
	"context"
	"time"

	"syncer/internal/core"
	"syncer/internal/svc"
	"syncer/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RotationCloseReportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRotationCloseReportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RotationCloseReportLogic {
	return &RotationCloseReportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// RotationCloseReport 收盘日报（Close Report，issue #12），一条链：
// ① 增量同步（幂等，已是最新则短路）→ ② Slippage Diff（官方日线 vs 盘中快照）
// → ③ 以官方收盘作当日第 20 点重算信号卡片。
// 薄壳：计算都在 core（Syncer / BuildSlippageDiffs / SignalComputer.ComputeCloseSignal，
// 均已单测覆盖），这里只做标的解析、当日数据对齐与 JSON 适配。
func (l *RotationCloseReportLogic) RotationCloseReport() (resp *types.CloseReportResp, err error) {
	instruments, err := l.svcCtx.Store.ListSyncEnabled(l.ctx)
	if err != nil {
		return nil, err
	}
	tsCodes := make([]string, 0, len(instruments))
	names := make(map[string]string, len(instruments))
	for _, inst := range instruments {
		tsCodes = append(tsCodes, inst.TsCode)
		names[inst.TsCode] = inst.Name
	}

	resp = &types.CloseReportResp{}

	// ① 增量同步（与 SyncEtfDaily 同一入口，限定轮动标的）。
	sum := l.svcCtx.Syncer.Run(l.ctx, tsCodes)
	resp.Sync = types.SyncResp{Total: sum.Total, Success: sum.Success}
	for _, r := range sum.Results {
		resp.Sync.Results = append(resp.Sync.Results, types.SyncResultItem{
			TsCode:        r.TsCode,
			StartDate:     r.StartDate,
			EndDate:       r.EndDate,
			Fetched:       r.Fetched,
			Upserted:      r.Upserted,
			DailyFetched:  r.DailyFetched,
			AdjFetched:    r.AdjFetched,
			DailyUpserted: r.DailyUpserted,
			AdjUpserted:   r.AdjUpserted,
			Message:       r.Message,
		})
	}

	today := time.Now().Format("20060102")
	resp.TradeDate = today

	// ② 当日数据对齐：盘中快照按 (ts_codes, today) 读；官方日线取每标的末根且日期 = 今天。
	snaps, err := l.svcCtx.Store.IntradaySnapshots(l.ctx, tsCodes, today)
	if err != nil {
		return nil, err
	}
	resp.HasSnapshot = len(snaps) > 0
	snapByCode := make(map[string]core.IntradaySnapshot, len(snaps))
	for _, s := range snaps {
		snapByCode[s.TsCode] = s
	}

	bars := make(map[string]core.DailyBarAdj, len(tsCodes))
	for _, code := range tsCodes {
		latest, err := l.svcCtx.Store.RecentDaily(l.ctx, code, 1)
		if err != nil {
			return nil, err
		}
		if len(latest) == 1 && latest[0].TradeDate == today {
			bars[code] = latest[0]
			resp.TradingDay = true
		}
	}

	for _, d := range core.BuildSlippageDiffs(tsCodes, bars, snapByCode) {
		resp.Diffs = append(resp.Diffs, jsonDiff(d, names[d.TsCode]))
	}

	// ③ 官方收盘重算（复用 SignalComputer，与盘中信号同一打分口径）。
	report, err := l.svcCtx.SignalComputer.ComputeCloseSignal(l.ctx, tsCodes, today)
	if err != nil {
		return nil, err
	}
	for _, card := range report.Cards {
		resp.Cards = append(resp.Cards, types.SignalCardItem{
			TsCode:   card.TsCode,
			Name:     names[card.TsCode],
			Score:    jsonFloat(card.Score),
			YZVol:    jsonFloat(card.YZVol),
			Quantile: jsonFloat(card.Quantile),
			Weight:   card.Weight,
			Rank:     card.Rank,
			Stale:    card.Stale,
			Message:  card.Message,
		})
	}
	return resp, nil
}

// jsonDiff 把 core 的 InstrumentDiff 适配为 JSON 结构：无差值时各字段为 null。
func jsonDiff(d core.InstrumentDiff, name string) types.SlippageDiffItem {
	item := types.SlippageDiffItem{
		TsCode:    d.TsCode,
		Name:      name,
		Available: d.Available,
		Message:   d.Message,
	}
	if d.Available {
		item.Open = &types.DiffField{Abs: d.Open.Abs, Bps: d.Open.Bps}
		item.High = &types.DiffField{Abs: d.High.Abs, Bps: d.High.Bps}
		item.Low = &types.DiffField{Abs: d.Low.Abs, Bps: d.Low.Bps}
		item.Close = &types.DiffField{Abs: d.Close.Abs, Bps: d.Close.Bps}
		item.MeanBps = jsonFloat(d.MeanBps)
	}
	return item
}
