package logic

import (
	"context"
	"math"

	"syncer/internal/core"
	"syncer/internal/svc"
	"syncer/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type RotationSignalLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRotationSignalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RotationSignalLogic {
	return &RotationSignalLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// RotationSignal 计算全部启用同步标的的盘中信号卡片并落库盘中快照。
// 薄壳：计算与落库都在 core.SignalComputer（已单测覆盖），这里只做标的解析与 JSON 适配。
func (l *RotationSignalLogic) RotationSignal() (resp *types.SignalResp, err error) {
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

	report, err := l.svcCtx.SignalComputer.ComputeSignal(l.ctx, tsCodes)
	if err != nil {
		return nil, err
	}

	resp = &types.SignalResp{
		TradeDate:    report.TradeDate,
		SnapshotDate: report.SnapshotDate,
		TradingDay:   report.SnapshotDate != "" && report.SnapshotDate == report.TradeDate,
	}
	for _, card := range report.Cards {
		resp.Cards = append(resp.Cards, toSignalCardItem(card, names[card.TsCode]))
	}
	return resp, nil
}

// toSignalCardItem 把 core.SignalCard 适配为 JSON 结构（两个端点共用）。
func toSignalCardItem(card core.SignalCard, name string) types.SignalCardItem {
	return types.SignalCardItem{
		TsCode:   card.TsCode,
		Name:     name,
		Score:    jsonFloat(card.Score),
		YZVol:    jsonFloat(card.YZVol),
		Quantile: jsonFloat(card.Quantile),
		Weight:   card.Weight,
		Rank:     card.Rank,
		Stale:    card.Stale,
		Message:  card.Message,
	}
}

// jsonFloat 把 NaN（历史不足）转为 nil，使 JSON 序列化为 null（encoding/json 无法编码 NaN）。
func jsonFloat(v float64) *float64 {
	if math.IsNaN(v) {
		return nil
	}
	return &v
}
