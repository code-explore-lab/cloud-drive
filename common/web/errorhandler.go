package web

import (
	"context"
	"net/http"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrorBody 是所有失败响应的统一结构。
// 成功响应直接返回资源对象本身，失败响应一定是这个结构（见 docs/api.md）。
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// ErrorHandler 把 gRPC 状态错误映射成 HTTP 语义状态与稳定错误码。
//
// 这样做的好处：下游 RPC 用 codes.Xxx 表达业务语义，HTTP 层不需要逐个接口判断，
// 客户端也能拿到稳定的 code 字符串做分支。
func ErrorHandler(ctx context.Context, err error) (int, any) {
	code := codeOf(err)
	return httpStatusOf(code), ErrorBody{
		Code:      codeName(code),
		Message:   messageOf(err),
		RequestID: RequestIDFromCtx(ctx),
	}
}

// Setup 注册全局错误处理器与请求 ID 中间件，每个 API 服务在 main 里调用一次。
func Setup() {
	httpx.SetErrorHandlerCtx(ErrorHandler)
}

func codeOf(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if st, ok := status.FromError(err); ok {
		return st.Code()
	}
	// 非 gRPC 错误：参数绑定失败等都由 go-zero 生成普通 error，
	// 统一当作调用方错误处理，而不是 500。
	return codes.InvalidArgument
}

func messageOf(err error) string {
	if st, ok := status.FromError(err); ok {
		msg := st.Message()
		if msg == "" {
			msg = codeName(st.Code())
		}
		return msg
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func httpStatusOf(code codes.Code) int {
	switch code {
	case codes.OK:
		return http.StatusOK
	case codes.InvalidArgument, codes.OutOfRange:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition:
		return http.StatusConflict
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.Canceled:
		return 499 // client closed request
	default:
		return http.StatusInternalServerError
	}
}

func codeName(code codes.Code) string {
	switch code {
	case codes.OK:
		return "ok"
	case codes.InvalidArgument:
		return "invalid_argument"
	case codes.OutOfRange:
		return "out_of_range"
	case codes.Unauthenticated:
		return "unauthenticated"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.NotFound:
		return "not_found"
	case codes.AlreadyExists:
		return "already_exists"
	case codes.Aborted:
		return "aborted"
	case codes.FailedPrecondition:
		return "failed_precondition"
	case codes.ResourceExhausted:
		return "resource_exhausted"
	case codes.Unimplemented:
		return "unimplemented"
	case codes.Unavailable:
		return "unavailable"
	case codes.DeadlineExceeded:
		return "deadline_exceeded"
	case codes.Canceled:
		return "canceled"
	default:
		return "internal"
	}
}

// LogError 记录错误日志时附带请求 ID，但绝不打印敏感字段。
func LogError(ctx context.Context, msg string, err error) {
	logx.WithContext(ctx).Errorf("%s: %v request_id=%s", msg, err, RequestIDFromCtx(ctx))
}

// WriteError 用于个别需要显式 HTTP 状态码的场景（例如 Range 请求返回 416），
// 其余情况应直接返回 error 交给全局 ErrorHandler。
func WriteError(ctx context.Context, w http.ResponseWriter, httpStatus int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	httpx.OkJsonCtx(ctx, w, ErrorBody{
		Code:      code,
		Message:   message,
		RequestID: RequestIDFromCtx(ctx),
	})
}
