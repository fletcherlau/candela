// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package handler

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
	"syncer/internal/logic"
	"syncer/internal/svc"
	"syncer/internal/types"
)

func SyncSwIndexDailyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SyncReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := logic.NewSyncSwIndexDailyLogic(r.Context(), svcCtx)
		resp, err := l.SyncSwIndexDaily(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
