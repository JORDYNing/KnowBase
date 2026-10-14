package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// GetFeishuConfig godoc
// @Summary      获取飞书登录配置
// @Description  返回飞书登录是否启用，供前端决定是否展示飞书登录入口
// @Tags         认证
// @Produce      json
// @Success      200  {object}  types.FeishuConfigResponse
// @Router       /auth/feishu/config [get]
func (h *AuthHandler) GetFeishuConfig(c *gin.Context) {
	enabled := false
	if h.configInfo != nil && h.configInfo.FeishuLogin != nil {
		enabled = h.configInfo.FeishuLogin.FeishuLoginReady()
	}
	c.JSON(http.StatusOK, &types.FeishuConfigResponse{
		Success: true,
		Enabled: enabled,
	})
}

// FeishuStart godoc
// @Summary      发起飞书登录（直接 302）
// @Description  直接 302 到飞书授权页。OAuth redirect_uri 始终指向本后端
// 回调（需在飞书开放平台登记）；可选 return_origin 仅用于登录完成后跳回 SPA。
// @Tags         认证
// @Success      302
// @Router       /auth/feishu/start [get]
func (h *AuthHandler) FeishuStart(c *gin.Context) {
	ctx := c.Request.Context()
	returnOrigin := sanitizeReturnOrigin(c.Query("return_origin"))
	resp, err := h.userService.GetFeishuAuthorizationURL(ctx, feishuCallbackURL(c), returnOrigin)
	if err != nil {
		logger.Errorf(ctx, "Failed to generate Feishu authorization URL: %v", err)
		appErr := errors.NewForbiddenError("Feishu authorization unavailable").WithDetails(err.Error())
		c.Error(appErr)
		return
	}
	setOIDCNonceCookie(c, resp.Nonce)
	c.Redirect(http.StatusFound, resp.AuthorizationURL)
}

// FeishuRedirectCallback godoc
// @Summary      飞书登录重定向回调
// @Description  接收飞书授权回调，完成 code 交换后重定向回前端并携带本地 JWT
// @Tags         认证
// @Param        code   query string false "授权码"
// @Param        state  query string false "状态"
// @Param        error  query string false "错误码"
// @Success      302
// @Router       /auth/feishu/callback [get]
func (h *AuthHandler) FeishuRedirectCallback(c *gin.Context) {
	ctx := c.Request.Context()
	frontendRedirectURI := feishuFrontendRedirect("", "")

	if providerError := strings.TrimSpace(c.Query("error")); providerError != "" {
		redirectURL := frontendRedirectURI + "#oidc_error=" + urlQueryEscape(providerError)
		if description := strings.TrimSpace(c.Query("error_description")); description != "" {
			redirectURL += "&oidc_error_description=" + urlQueryEscape(description)
		}
		c.Redirect(http.StatusFound, redirectURL)
		return
	}

	state := strings.TrimSpace(c.Query("state"))
	decodedState, err := decodeOIDCState(state, c.Request)
	if err != nil {
		logger.Errorf(ctx, "Failed to decode Feishu state: %v", err)
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("invalid_state"))
		return
	}
	if decodedState != nil {
		frontendRedirectURI = feishuFrontendRedirect(decodedState.RedirectURI, decodedState.ReturnOrigin)
	}
	c.SetCookie(oidcNonceCookieName, "", -1, "/", "", false, true)

	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("missing_code"))
		return
	}

	resp, err := h.userService.LoginWithFeishu(
		ctx,
		code,
		strings.TrimSpace(decodedState.RedirectURI),
		h.resolveDefaultTenantMode(ctx),
	)
	if err != nil {
		logger.Errorf(ctx, "Failed to complete Feishu login via redirect callback: %v", err)
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("login_failed")+"&oidc_error_description="+urlQueryEscape(err.Error()))
		return
	}
	if !resp.Success {
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("login_failed")+"&oidc_error_description="+urlQueryEscape(resp.Message))
		return
	}

	payload, err := encodeOIDCCallbackPayload(resp)
	if err != nil {
		logger.Errorf(ctx, "Failed to encode Feishu callback payload: %v", err)
		c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("payload_encode_failed"))
		return
	}

	c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_result="+urlQueryEscape(payload))
}

// feishuCallbackURL is the OAuth redirect_uri sent to Feishu. It always points
// at this backend (request Host / X-Forwarded-*), never at the Vite SPA origin.
// Register the exact URL in the Feishu developer console, e.g.
// http://127.0.0.1:8080/api/v1/auth/feishu/callback
func feishuCallbackURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	return scheme + "://" + host + "/api/v1/auth/feishu/callback"
}

// sanitizeReturnOrigin accepts only an http(s) origin. Used after Feishu
// callback to bounce the browser back to the SPA that started login.
func sanitizeReturnOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func feishuFrontendRedirect(callbackURL, returnOrigin string) string {
	if origin := sanitizeReturnOrigin(returnOrigin); origin != "" {
		return origin + "/"
	}
	u, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "/"
	}
	return u.Scheme + "://" + u.Host + "/"
}
