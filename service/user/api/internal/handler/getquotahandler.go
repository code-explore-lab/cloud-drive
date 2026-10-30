// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package handler

import (
	"net/http"

	"cloud-drive/service/user/api/internal/logic"
	"cloud-drive/service/user/api/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetQuotaHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewGetQuotaLogic(r.Context(), svcCtx)
		resp, err := l.GetQuota()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
