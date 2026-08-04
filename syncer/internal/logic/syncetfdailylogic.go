package logic

import (
	"context"

	"syncer/internal/svc"
	"syncer/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SyncEtfDailyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSyncEtfDailyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncEtfDailyLogic {
	return &SyncEtfDailyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SyncEtfDailyLogic) SyncEtfDaily(req *types.SyncReq) (resp *types.SyncResp, err error) {
	sum := l.svcCtx.Syncer.Run(l.ctx, req.TsCodes)

	resp = &types.SyncResp{
		Total:   sum.Total,
		Success: sum.Success,
	}
	for _, r := range sum.Results {
		resp.Results = append(resp.Results, types.SyncResultItem{
			TsCode:    r.TsCode,
			StartDate: r.StartDate,
			EndDate:   r.EndDate,
			Fetched:   r.Fetched,
			Upserted:  r.Upserted,
			Message:   r.Message,
		})
	}
	return resp, nil
}
