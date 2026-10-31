package handler

import (
	"net/http"
	"time"

	"cloud-drive/service/user/api/internal/svc"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// HealthHandler 是存活探针，供容器健康检查和 K8s livenessProbe 使用。
func HealthHandler(_ *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.OkJsonCtx(r.Context(), w, map[string]any{
			"status": "ok",
			"time":   time.Now().Unix(),
		})
	}
}
