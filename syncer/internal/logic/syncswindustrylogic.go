package logic

import (
	"context"

	"syncer/internal/svc"
	"syncer/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type SyncSwIndustryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSyncSwIndustryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncSwIndustryLogic {
	return &SyncSwIndustryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SyncSwIndustryLogic) SyncSwIndustry() (resp *types.SWIndustrySyncResp, err error) {
	sum := l.svcCtx.SWSyncer.RunIndustry(l.ctx)
	return &types.SWIndustrySyncResp{
		DictFetched:    sum.DictFetched,
		DictUpserted:   sum.DictUpserted,
		MemberFetched:  sum.MemberFetched,
		MemberUpserted: sum.MemberUpserted,
		Message:        sum.Message,
	}, nil
}
