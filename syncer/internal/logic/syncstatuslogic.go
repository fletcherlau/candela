package logic

import (
	"context"

	"syncer/internal/svc"
	"syncer/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SyncStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSyncStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncStatusLogic {
	return &SyncStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SyncStatusLogic) SyncStatus() (resp *types.StatusResp, err error) {
	statuses, err := l.svcCtx.Store.Statuses(l.ctx)
	if err != nil {
		return nil, err
	}

	resp = &types.StatusResp{}
	for _, st := range statuses {
		resp.Instruments = append(resp.Instruments, types.InstrumentStatusItem{
			TsCode:          st.TsCode,
			Name:            st.Name,
			SyncEnabled:     st.SyncEnabled,
			LatestTradeDate: st.LatestTradeDate,
			LatestAdjDate:   st.LatestAdjDate,
			DailyRows:       st.DailyRows,
			AdjRows:         st.AdjRows,
		})
	}
	return resp, nil
}
