// Package errors 提供 yunmao 错误码与统一错误信封（参见 04-设备接入数据模型与API边界.md 第 11 节）。
package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Code string

const (
	AuthLoginRequired Code = "AUTH.LOGIN_REQUIRED"
	AuthTokenExpired  Code = "AUTH.TOKEN_EXPIRED"
	AuthForbidden     Code = "AUTH.FORBIDDEN"

	UserNotFound Code = "USER.NOT_FOUND"
	RoomNotFound Code = "ROOM.NOT_FOUND"
	RoomOffline  Code = "ROOM.OFFLINE"

	FeedCooldownNotFinished Code = "FEED.COOLDOWN_NOT_FINISHED"
	FeedHealthLimitHit      Code = "FEED.HEALTH_LIMIT_HIT"
	FeedDeviceOffline       Code = "FEED.DEVICE_OFFLINE"
	FeedNoFeedWindow        Code = "FEED.NO_FEED_WINDOW"
	FeedDuplicateRequest    Code = "FEED.DUPLICATE_REQUEST"

	DeviceUnbound     Code = "DEVICE.UNBOUND"
	DeviceErrorJammed Code = "DEVICE.ERROR_JAMMED"

	MediaStreamOffline      Code = "MEDIA.STREAM_OFFLINE"
	MediaProfileUnavailable Code = "MEDIA.PROFILE_UNAVAILABLE"

	PayOrderPaid      Code = "PAY.ORDER_PAID"
	PayAmountMismatch Code = "PAY.AMOUNT_MISMATCH"
	PayChannelFailed  Code = "PAY.CHANNEL_FAILED"

	RiskActionBlocked Code = "RISK.ACTION_BLOCKED"

	SystemRateLimited           Code = "SYSTEM.RATE_LIMITED"
	SystemInternal              Code = "SYSTEM.INTERNAL"
	SystemDependencyUnavailable Code = "SYSTEM.DEPENDENCY_UNAVAILABLE"
)

// HTTPStatus 返回错误码对应的 HTTP 状态码。
func (c Code) HTTPStatus() int {
	switch c {
	case AuthLoginRequired, AuthTokenExpired:
		return http.StatusUnauthorized
	case AuthForbidden, RiskActionBlocked:
		return http.StatusForbidden
	case UserNotFound, RoomNotFound:
		return http.StatusNotFound
	case FeedCooldownNotFinished, FeedHealthLimitHit,
		FeedDeviceOffline, FeedNoFeedWindow, FeedDuplicateRequest,
		DeviceUnbound, DeviceErrorJammed,
		PayOrderPaid, PayAmountMismatch, PayChannelFailed:
		return http.StatusConflict
	case RoomOffline, MediaStreamOffline, MediaProfileUnavailable, SystemDependencyUnavailable:
		return http.StatusServiceUnavailable
	case SystemRateLimited:
		return http.StatusTooManyRequests
	case SystemInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

// Envelope 统一错误响应。
type Envelope struct {
	Error Body `json:"error"`
}
type Body struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
}

func New(code Code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// AppError 业务错误，实现 error 接口。
type AppError struct {
	Code        Code
	Message     string
	Remediation string
	TraceID     string
}

func (e *AppError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) WithTrace(id string) *AppError {
	e.TraceID = id
	return e
}

func (e *AppError) MarshalJSON() ([]byte, error) {
	return json.Marshal(Envelope{
		Error: Body{
			Code:        string(e.Code),
			Message:     e.Message,
			Remediation: e.Remediation,
			TraceID:     e.TraceID,
		},
	})
}

// AsAppError 把任意 error 转为 AppError；非 AppError 视为 SYSTEM.INTERNAL。
func AsAppError(err error) *AppError {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*AppError); ok {
		return appErr
	}
	return New(SystemInternal, err.Error())
}
