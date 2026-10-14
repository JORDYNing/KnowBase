package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

const feishuNoreplyEmailDomain = "users.noreply.feishu"

var (
	// ErrFeishuLoginDisabled is returned when Feishu login is not ready.
	ErrFeishuLoginDisabled = errors.New("Feishu login is disabled")
	// ErrFeishuIdentityConflict is returned when an email-matched account is
	// already bound to a different Feishu open_id.
	ErrFeishuIdentityConflict = errors.New("account is already linked to a different Feishu identity")
)

type feishuTokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
}

type feishuTokenResponse struct {
	Code                  int    `json:"code"`
	Msg                   string `json:"msg"`
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	TokenType             string `json:"token_type"`
	Scope                 string `json:"scope"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

type feishuUserInfoResponse struct {
	Code int                 `json:"code"`
	Msg  string              `json:"msg"`
	Data *feishuUserInfoData `json:"data"`
}

type feishuUserInfoData struct {
	Name            string `json:"name"`
	EnName          string `json:"en_name"`
	AvatarURL       string `json:"avatar_url"`
	AvatarThumb     string `json:"avatar_thumb"`
	OpenID          string `json:"open_id"`
	UnionID         string `json:"union_id"`
	Email           string `json:"email"`
	EnterpriseEmail string `json:"enterprise_email"`
	UserID          string `json:"user_id"`
}

// GetFeishuAuthorizationURL builds the Feishu OAuth authorization URL and a
// signed state payload (nonce + redirect_uri [+ optional return_origin]).
func (s *userService) GetFeishuAuthorizationURL(ctx context.Context, redirectURI, returnOrigin string) (*types.OIDCAuthURLResponse, error) {
	cfg, err := s.getFeishuLoginConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(redirectURI) == "" {
		return nil, errors.New("redirect_uri is required")
	}
	if err := validateOIDCEndpoint("feishu authorization", cfg.AuthorizationEndpoint, true); err != nil {
		return nil, err
	}

	nonce, err := generateRandomString(24)
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}
	state, err := secutils.SignOIDCState(&secutils.OIDCStatePayload{
		Nonce:        nonce,
		RedirectURI:  strings.TrimSpace(redirectURI),
		ReturnOrigin: strings.TrimSpace(returnOrigin),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode Feishu state: %w", err)
	}

	query := url.Values{}
	query.Set("client_id", cfg.AppID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("state", state)
	if len(cfg.Scopes) > 0 {
		query.Set("scope", strings.Join(cfg.Scopes, " "))
	}

	authURL := cfg.AuthorizationEndpoint
	if strings.Contains(authURL, "?") {
		authURL += "&" + query.Encode()
	} else {
		authURL += "?" + query.Encode()
	}

	return &types.OIDCAuthURLResponse{
		Success:          true,
		AuthorizationURL: authURL,
		State:            state,
		Nonce:            nonce,
	}, nil
}

// LoginWithFeishu exchanges the Feishu authorization code, resolves or
// provisions a local user, and returns the same callback payload shape as OIDC.
func (s *userService) LoginWithFeishu(
	ctx context.Context,
	code, redirectURI string,
	provisioning types.TenantProvisioningMode,
) (*types.OIDCCallbackResponse, error) {
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("code is required")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return nil, errors.New("redirect_uri is required")
	}

	cfg, err := s.getFeishuLoginConfig()
	if err != nil {
		return nil, err
	}

	tokenResp, err := s.exchangeFeishuCode(ctx, cfg, code, redirectURI)
	if err != nil {
		return nil, err
	}
	info, err := s.fetchFeishuUserInfo(ctx, cfg, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}
	openID := strings.TrimSpace(info.OpenID)
	if openID == "" {
		return nil, errors.New("Feishu user_info missing open_id")
	}

	user, isNewUser, err := s.resolveFeishuUser(ctx, info, types.TenantProvisioningTenantless)
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return &types.OIDCCallbackResponse{Success: false, Message: "Account is disabled"}, nil
	}

	if err := s.applyFeishuProfile(ctx, user, info); err != nil {
		return nil, err
	}

	// Enrollment is completed only after Feishu verified the identity. A
	// missing/disabled target leaves a valid tenantless session for the status UI.
	if s.joinRequestService == nil {
		return nil, errors.New("team enrollment service unavailable")
	}
	if err := s.joinRequestService.EnsureFeishuApplication(ctx, user, feishuVerifiedEmail(info)); err != nil && !errors.Is(err, types.ErrJoinTargetUnavailable) {
		return nil, err
	}
	user, err = s.userRepo.GetUserByID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	resolvedTenantID := s.resolveLoginTenantID(ctx, user)
	accessToken, refreshToken, err := s.generateTokensForTenant(ctx, user, resolvedTenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate local tokens: %w", err)
	}

	var tenant *types.Tenant
	if resolvedTenantID > 0 {
		if t, terr := s.tenantService.GetTenantByID(ctx, resolvedTenantID); terr == nil && t != nil && strings.EqualFold(strings.TrimSpace(t.Status), "active") {
			tenant = t
		} else {
			logger.Warnf(ctx, "Feishu login: failed to load tenant %d for user %s: %v",
				resolvedTenantID, user.ID, terr)
		}
	}
	memberships := s.buildMembershipsForUser(ctx, user, tenant)

	return &types.OIDCCallbackResponse{
		Success:      true,
		Message:      "登录成功",
		User:         user,
		Tenant:       tenant,
		Memberships:  memberships,
		Token:        accessToken,
		RefreshToken: refreshToken,
		IsNewUser:    isNewUser,
	}, nil
}

func (s *userService) getFeishuLoginConfig() (*config.FeishuLoginConfig, error) {
	if s.config == nil || s.config.FeishuLogin == nil || !s.config.FeishuLogin.FeishuLoginReady() {
		return nil, ErrFeishuLoginDisabled
	}
	cfg := *s.config.FeishuLogin
	if strings.TrimSpace(cfg.AuthorizationEndpoint) == "" {
		cfg.AuthorizationEndpoint = config.DefaultFeishuAuthorizeURL
	}
	if strings.TrimSpace(cfg.TokenEndpoint) == "" {
		cfg.TokenEndpoint = config.DefaultFeishuTokenURL
	}
	if strings.TrimSpace(cfg.UserInfoEndpoint) == "" {
		cfg.UserInfoEndpoint = config.DefaultFeishuUserInfoURL
	}
	return &cfg, nil
}

func (s *userService) exchangeFeishuCode(
	ctx context.Context,
	cfg *config.FeishuLoginConfig,
	code, redirectURI string,
) (*feishuTokenResponse, error) {
	if err := validateOIDCEndpoint("feishu token", cfg.TokenEndpoint, true); err != nil {
		return nil, err
	}

	body, err := json.Marshal(feishuTokenRequest{
		GrantType:    "authorization_code",
		ClientID:     cfg.AppID,
		ClientSecret: cfg.AppSecret,
		Code:         code,
		RedirectURI:  redirectURI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode Feishu token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create Feishu token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")

	resp, err := newOIDCHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange Feishu code: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 1<<20)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read Feishu token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Feishu token exchange failed: status=%d", resp.StatusCode)
	}

	var tokenResp feishuTokenResponse
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode Feishu token response: %w", err)
	}
	if tokenResp.Code != 0 {
		return nil, fmt.Errorf("Feishu token exchange failed: code=%d", tokenResp.Code)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, errors.New("Feishu token response missing access_token")
	}
	return &tokenResp, nil
}

func (s *userService) fetchFeishuUserInfo(
	ctx context.Context,
	cfg *config.FeishuLoginConfig,
	accessToken string,
) (*feishuUserInfoData, error) {
	if err := validateOIDCEndpoint("feishu userinfo", cfg.UserInfoEndpoint, true); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.UserInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := newOIDCHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Feishu user_info: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 1<<20)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read Feishu user_info: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Feishu user_info failed: status=%d", resp.StatusCode)
	}

	var envelope feishuUserInfoResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("failed to decode Feishu user_info: %w", err)
	}
	if envelope.Code != 0 || envelope.Data == nil {
		return nil, fmt.Errorf("Feishu user_info failed: code=%d", envelope.Code)
	}
	return envelope.Data, nil
}

func (s *userService) bindFeishuIdentity(ctx context.Context, user *types.User, openID string) (*types.User, error) {
	binder, ok := s.userRepo.(interfaces.FeishuIdentityRepository)
	if !ok {
		return nil, errors.New("atomic Feishu identity repository unavailable")
	}
	bound, err := binder.BindFeishuIdentity(ctx, user.ID, openID)
	if errors.Is(err, apprepo.ErrFeishuIdentityAlreadyBound) {
		return nil, ErrFeishuIdentityConflict
	}
	return bound, err
}

func (s *userService) resolveFeishuUser(ctx context.Context, info *feishuUserInfoData, provisioning types.TenantProvisioningMode) (*types.User, bool, error) {
	openID := strings.TrimSpace(info.OpenID)
	user, err := s.userRepo.GetUserByFeishuOpenID(ctx, openID)
	if err == nil && user != nil {
		return user, false, nil
	}
	if err != nil && !isUserLookupNotFound(err) {
		return nil, false, err
	}
	for _, email := range feishuCandidateEmails(info) {
		user, err = s.userRepo.GetUserByEmail(ctx, email)
		if err != nil && !isUserLookupNotFound(err) {
			return nil, false, err
		}
		if user == nil {
			continue
		}
		bound, err := s.bindFeishuIdentity(ctx, user, openID)
		return bound, false, err
	}
	oidcInfo := &types.OIDCUserInfo{Subject: openID, Username: feishuDisplayName(info), Email: feishuResolvedEmail(info)}
	user, err = s.provisionOIDCUser(ctx, oidcInfo, types.TenantProvisioningTenantless)
	if err != nil {
		// Registration can lose a concurrent first-login race on email. Re-read
		// the winning account and perform the same conditional identity binding.
		winner, lookupErr := s.userRepo.GetUserByEmail(ctx, oidcInfo.Email)
		if lookupErr != nil || winner == nil {
			return nil, false, err
		}
		bound, bindErr := s.bindFeishuIdentity(ctx, winner, openID)
		return bound, false, bindErr
	}
	bound, err := s.bindFeishuIdentity(ctx, user, openID)
	return bound, true, err
}

func (s *userService) applyFeishuProfile(ctx context.Context, user *types.User, info *feishuUserInfoData) error {
	avatar := strings.TrimSpace(info.AvatarURL)
	if avatar == "" {
		avatar = strings.TrimSpace(info.AvatarThumb)
	}
	if avatar == "" || avatar == user.Avatar {
		return nil
	}
	repo, ok := s.userRepo.(interfaces.FeishuIdentityRepository)
	if !ok {
		return errors.New("atomic Feishu identity repository unavailable")
	}
	if err := repo.UpdateFeishuAvatar(ctx, user.ID, user.FeishuOpenID, avatar); err != nil {
		return err
	}
	user.Avatar = avatar
	return nil
}

// Enterprise email is authoritative when present. Never bootstrap from a
// secondary personal address if Feishu supplied a different enterprise one.
func feishuVerifiedEmail(info *feishuUserInfoData) string {
	if email := strings.TrimSpace(info.EnterpriseEmail); email != "" {
		return strings.ToLower(email)
	}
	return strings.ToLower(strings.TrimSpace(info.Email))
}

func feishuCandidateEmails(info *feishuUserInfoData) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range []string{info.EnterpriseEmail, info.Email} {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	return out
}

func feishuResolvedEmail(info *feishuUserInfoData) string {
	if emails := feishuCandidateEmails(info); len(emails) > 0 {
		return emails[0]
	}
	openID := strings.TrimSpace(info.OpenID)
	return openID + "@" + feishuNoreplyEmailDomain
}

func feishuDisplayName(info *feishuUserInfoData) string {
	for _, candidate := range []string{info.Name, info.EnName} {
		if name := strings.TrimSpace(candidate); name != "" {
			return name
		}
	}
	if emails := feishuCandidateEmails(info); len(emails) > 0 {
		return strings.Split(emails[0], "@")[0]
	}
	return "feishu-user"
}
