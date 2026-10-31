// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package handler

import (
	"net/http"

	"cloud-drive/service/file/api/internal/logic"
	"cloud-drive/service/file/api/internal/svc"
	"cloud-drive/service/file/api/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func MoveFileHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.MoveFileReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := logic.NewMoveFileLogic(r.Context(), svcCtx)
		err := l.MoveFile(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.Ok(w)
		}
	}
}
