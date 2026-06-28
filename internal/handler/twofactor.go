package handler

import (
	"errors"
	"net/http"

	"flist/internal/middleware"
	"flist/internal/service"
)

// TwoFactorHandler 处理 2FA 管理相关接口。
type TwoFactorHandler struct {
	auth *service.AuthService
}

// NewTwoFactorHandler 构造 2FA 处理器。
func NewTwoFactorHandler(auth *service.AuthService) *TwoFactorHandler {
	return &TwoFactorHandler{auth: auth}
}

type twoFactorSetupResponse struct {
	Secret string `json:"secret"`
	QRCode string `json:"qr_code"`
}

// Setup 处理 POST /api/2fa/setup。
// 生成新的 TOTP 密钥（此时 2FA 未启用），返回 secret 和 QR 码供前端展示。
func (h *TwoFactorHandler) Setup(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		failUnauthorized(w)
		return
	}
	secret, qrCode, err := h.auth.SetupTwoFactor(user.ID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTOTPAlreadyEnabled):
			Fail(w, http.StatusConflict, CodeTOTPAlreadyEnabled, "totp_already_enabled")
		default:
			failInternal(w)
		}
		return
	}
	OK(w, twoFactorSetupResponse{Secret: secret, QRCode: qrCode})
}

type twoFactorCodeRequest struct {
	Code string `json:"code"`
}

// Enable 处理 POST /api/2fa/enable。
// 用户扫码后输入验证码确认启用。
func (h *TwoFactorHandler) Enable(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		failUnauthorized(w)
		return
	}
	var req twoFactorCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "bad_request")
		return
	}
	if req.Code == "" {
		failBadRequest(w, "code required")
		return
	}
	err := h.auth.EnableTwoFactor(user.ID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTOTP):
			Fail(w, http.StatusUnauthorized, CodeInvalidTOTP, "invalid_totp")
		case errors.Is(err, service.ErrTOTPAlreadyEnabled):
			Fail(w, http.StatusConflict, CodeTOTPAlreadyEnabled, "totp_already_enabled")
		case errors.Is(err, service.ErrTOTPNotSetup):
			Fail(w, http.StatusBadRequest, CodeBadRequest, "totp_not_setup")
		default:
			failInternal(w)
		}
		return
	}
	OK(w, nil)
}

// Disable 处理 POST /api/2fa/disable。
// 用户输入验证码确认关闭 2FA。
func (h *TwoFactorHandler) Disable(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		failUnauthorized(w)
		return
	}
	var req twoFactorCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failBadRequest(w, "bad_request")
		return
	}
	if req.Code == "" {
		failBadRequest(w, "code required")
		return
	}
	err := h.auth.DisableTwoFactor(user.ID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTOTP):
			Fail(w, http.StatusUnauthorized, CodeInvalidTOTP, "invalid_totp")
		case errors.Is(err, service.ErrTOTPNotEnabled):
			Fail(w, http.StatusConflict, CodeTOTPNotEnabled, "totp_not_enabled")
		default:
			failInternal(w)
		}
		return
	}
	OK(w, nil)
}

type twoFactorStatusResponse struct {
	Enabled bool `json:"enabled"`
}

// Status 处理 GET /api/2fa/status。
func (h *TwoFactorHandler) Status(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		failUnauthorized(w)
		return
	}
	enabled, err := h.auth.GetTwoFactorStatus(user.ID)
	if err != nil {
		failInternal(w)
		return
	}
	OK(w, twoFactorStatusResponse{Enabled: enabled})
}
