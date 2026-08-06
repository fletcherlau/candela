package logic

import (
	"context"

	"syncer/internal/svc"
	"syncer/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SwStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSwStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SwStatusLogic {
	return &SwStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SwStatusLogic) SwStatus() (resp *types.SWStatusResp, err error) {
	resp = &types.SWStatusResp{}

	if resp.IndustryRows, err = l.svcCtx.SWStore.CountSWIndustries(l.ctx); err != nil {
		return nil, err
	}
	if resp.MemberRows, err = l.svcCtx.SWStore.CountSWMembers(l.ctx); err != nil {
		return nil, err
	}
	statuses, err := l.svcCtx.SWStore.SWIndexStatuses(l.ctx)
	if err != nil {
		return nil, err
	}
	for _, st := range statuses {
		resp.Indexes = append(resp.Indexes, types.SWIndexStatusItem{
			TsCode:          st.TsCode,
			Name:            st.Name,
			Level:           st.Level,
			LatestTradeDate: st.LatestTradeDate,
			DailyRows:       st.DailyRows,
		})
	}
	return resp, nil
}
