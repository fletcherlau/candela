package logic

import (
	"context"

	"syncer/internal/svc"
	"syncer/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SyncSwIndexDailyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSyncSwIndexDailyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncSwIndexDailyLogic {
	return &SyncSwIndexDailyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SyncSwIndexDailyLogic) SyncSwIndexDaily(req *types.SyncReq) (resp *types.SWDailySyncResp, err error) {
	sum := l.svcCtx.SWSyncer.RunDaily(l.ctx, req.TsCodes)

	resp = &types.SWDailySyncResp{
		Total:   sum.Total,
		Success: sum.Success,
	}
	for _, r := range sum.Results {
		resp.Results = append(resp.Results, types.SWDailyResultItem{
			TsCode:    r.TsCode,
			Name:      r.Name,
			Level:     r.Level,
			StartDate: r.StartDate,
			EndDate:   r.EndDate,
			Fetched:   r.Fetched,
			Upserted:  r.Upserted,
			Message:   r.Message,
		})
	}
	return resp, nil
}
