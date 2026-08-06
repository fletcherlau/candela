// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.2

package handler

import (
	"net/http"
	"strconv"

	"github.com/zeromicro/go-zero/rest/httpx"
	"syncer/internal/logic"
	"syncer/internal/svc"
)

func RotationSignalHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// persist 缺省 true（cron 14:45 推送保持盘中快照落库）；
		// 显式 persist=false（signal 聊天命令）只读计算，不写 intraday_snapshot。
		persist := true
		if v := r.URL.Query().Get("persist"); v != "" {
			if b, err := strconv.ParseBool(v); err == nil {
				persist = b
			}
		}
		l := logic.NewRotationSignalLogic(r.Context(), svcCtx)
		resp, err := l.RotationSignal(persist)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
