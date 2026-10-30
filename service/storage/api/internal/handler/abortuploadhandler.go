// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package handler

import (
	"net/http"

	"cloud-drive/service/storage/api/internal/logic"
	"cloud-drive/service/storage/api/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func AbortUploadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewAbortUploadLogic(r.Context(), svcCtx)
		resp, err := l.AbortUpload()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
