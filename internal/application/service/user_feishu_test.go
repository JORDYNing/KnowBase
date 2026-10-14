package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

type feishuTestUserRepo struct {
	usersByID    map[string]*types.User
	usersByEmail map[string]*types.User
	usersByOpen  map[string]*types.User
	createCalls  int
	updateCalls  int
}

func newFeishuTestUserRepo(users ...*types.User) *feishuTestUserRepo {
	r := &feishuTestUserRepo{
		usersByID:    map[string]*types.User{},
		usersByEmail: map[string]*types.User{},
		usersByOpen:  map[string]*types.User{},
	}
	for _, u := range users {
		cp := *u
		r.usersByID[u.ID] = &cp
		if u.Email != "" {
			r.usersByEmail[strings.ToLower(u.Email)] = &cp
		}
		if u.FeishuOpenID != "" {
			r.usersByOpen[u.FeishuOpenID] = &cp
		}
	}
	return r
}

func (r *feishuTestUserRepo) CreateUser(_ context.Context, user *types.User) error {
	r.createCalls++
	cp := *user
	r.usersByID[user.ID] = &cp
	r.usersByEmail[strings.ToLower(user.Email)] = &cp
	if user.FeishuOpenID != "" {
		r.usersByOpen[user.FeishuOpenID] = &cp
	}
	return nil
}
func (r *feishuTestUserRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	u, ok := r.usersByID[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	cp := *u
	return &cp, nil
}
func (r *feishuTestUserRepo) GetUsersByIDs(context.Context, []string) (map[string]*types.User, error) {
	return nil, nil
}
func (r *feishuTestUserRepo) GetUserByEmail(_ context.Context, email string) (*types.User, error) {
	u, ok := r.usersByEmail[strings.ToLower(email)]
	if !ok {
		return nil, errors.New("user not found")
	}
	cp := *u
	return &cp, nil
}
func (r *feishuTestUserRepo) GetUserByUsername(_ context.Context, username string) (*types.User, error) {
	for _, u := range r.usersByID {
		if u.Username == username {
			cp := *u
			return &cp, nil
		}
	}
	return nil, errors.New("user not found")
}
func (r *feishuTestUserRepo) GetUserByFeishuOpenID(_ context.Context, openID string) (*types.User, error) {
	u, ok := r.usersByOpen[openID]
	if !ok {
		return nil, errors.New("user not found")
	}
	cp := *u
	return &cp, nil
}
func (r *feishuTestUserRepo) GetUserByTenantID(context.Context, uint64) (*types.User, error) {
	return nil, errors.New("user not found")
}
func (r *feishuTestUserRepo) UpdateUser(_ context.Context, user *types.User) error {
	r.updateCalls++
	cp := *user
	r.usersByID[user.ID] = &cp
	r.usersByEmail[strings.ToLower(user.Email)] = &cp
	for k, v := range r.usersByOpen {
		if v.ID == user.ID {
			delete(r.usersByOpen, k)
		}
	}
	if user.FeishuOpenID != "" {
		r.usersByOpen[user.FeishuOpenID] = &cp
	}
	return nil
}
func (r *feishuTestUserRepo) DeleteUser(context.Context, string) error { return nil }
func (r *feishuTestUserRepo) ListUsers(context.Context, int, int) ([]*types.User, error) {
	return nil, nil
}
func (r *feishuTestUserRepo) ListSystemAdmins(context.Context, int, int) ([]*types.User, int64, error) {
	return nil, 0, nil
}
func (r *feishuTestUserRepo) RevokeSystemAdmin(context.Context, string, string) (*types.User, error) {
	return nil, nil
}
func (r *feishuTestUserRepo) SearchUsers(context.Context, string, int) ([]*types.User, error) {
	return nil, nil
}

type feishuTokenRepo struct{ stubAuthTokenRepo }

func withFeishuSSRFWhitelist(t *testing.T) {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
}

func newFeishuMockServer(t *testing.T, tokenCode int, userCode int, userData *feishuUserInfoData) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s, want POST", r.Method)
			}
			if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Fatalf("token Content-Type = %q, want application/json", ct)
			}
			var req feishuTokenRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode token body: %v", err)
			}
			if req.GrantType != "authorization_code" || req.Code == "" || req.RedirectURI == "" {
				t.Fatalf("unexpected token request: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(feishuTokenResponse{
				Code:        tokenCode,
				AccessToken: "u-test-token",
				TokenType:   "Bearer",
			})
		case strings.HasSuffix(r.URL.Path, "/user_info"):
			if got := r.Header.Get("Authorization"); got != "Bearer u-test-token" {
				t.Fatalf("Authorization = %q", got)
			}
			_ = json.NewEncoder(w).Encode(feishuUserInfoResponse{
				Code: userCode,
				Data: userData,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGetFeishuAuthorizationURL(t *testing.T) {
	svc := &userService{config: &config.Config{FeishuLogin: &config.FeishuLoginConfig{
		Enable:                true,
		AppID:                 "cli_test",
		AppSecret:             "secret",
		Scopes:                []string{"contact:user.base:readonly"},
		AuthorizationEndpoint: "https://accounts.feishu.cn/open-apis/authen/v1/authorize",
	}}}
	resp, err := svc.GetFeishuAuthorizationURL(context.Background(), "https://app.example/api/v1/auth/feishu/callback", "http://127.0.0.1:5173")
	if err != nil {
		t.Fatalf("GetFeishuAuthorizationURL: %v", err)
	}
	u, err := url.Parse(resp.AuthorizationURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != "cli_test" {
		t.Fatalf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Fatalf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("redirect_uri") != "https://app.example/api/v1/auth/feishu/callback" {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") == "" || resp.Nonce == "" {
		t.Fatalf("missing state/nonce")
	}
}

func TestGetFeishuAuthorizationURLDisabled(t *testing.T) {
	svc := &userService{config: &config.Config{}}
	_, err := svc.GetFeishuAuthorizationURL(context.Background(), "https://app.example/callback", "")
	if !errors.Is(err, ErrFeishuLoginDisabled) {
		t.Fatalf("error = %v, want ErrFeishuLoginDisabled", err)
	}
}

func TestExchangeFeishuCodeRejectsNonZero(t *testing.T) {
	withFeishuSSRFWhitelist(t)
	server := newFeishuMockServer(t, 9999, 0, nil)
	defer server.Close()

	svc := &userService{}
	cfg := &config.FeishuLoginConfig{
		AppID: "id", AppSecret: "secret",
		TokenEndpoint: server.URL + "/token",
	}
	_, err := svc.exchangeFeishuCode(context.Background(), cfg, "code", "https://app.example/callback")
	if err == nil || !strings.Contains(err.Error(), "code=9999") {
		t.Fatalf("error = %v, want code=9999", err)
	}
}

func TestLoginWithFeishuOpenIDHit(t *testing.T) {
	withFeishuSSRFWhitelist(t)
	existing := &types.User{
		ID: "u1", Email: "a@example.com", Username: "alice",
		FeishuOpenID: "ou_existing", IsActive: true, TenantID: 1,
		PasswordHash: "x",
	}
	repo := newFeishuTestUserRepo(existing)
	server := newFeishuMockServer(t, 0, 0, &feishuUserInfoData{
		OpenID: "ou_existing", Name: "Alice", AvatarURL: "https://img/a.png",
		EnterpriseEmail: "a@example.com",
	})
	defer server.Close()

	svc := &userService{
		userRepo:      repo,
		tokenRepo:     &feishuTokenRepo{},
		tenantService: &provisioningTenantService{},
		memberService: &membershipLookupService{byTenant: map[uint64]*types.TenantMember{
			1: {TenantID: 1, Status: types.TenantMemberStatusActive, Role: types.TenantRoleOwner},
		}},
		config: &config.Config{FeishuLogin: &config.FeishuLoginConfig{
			Enable: true, AppID: "id", AppSecret: "secret",
			TokenEndpoint:    server.URL + "/token",
			UserInfoEndpoint: server.URL + "/user_info",
		}},
	}

	resp, err := svc.LoginWithFeishu(context.Background(), "code", "https://app.example/callback", types.TenantProvisioningCreatePersonal)
	if err != nil {
		t.Fatalf("LoginWithFeishu: %v", err)
	}
	if !resp.Success || resp.IsNewUser || resp.Token == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.User.Avatar != "https://img/a.png" {
		t.Fatalf("avatar = %q", resp.User.Avatar)
	}
}

func TestLoginWithFeishuEmailLink(t *testing.T) {
	withFeishuSSRFWhitelist(t)
	existing := &types.User{
		ID: "u2", Email: "corp@example.com", Username: "corp",
		IsActive: true, TenantID: 2, PasswordHash: "x",
	}
	repo := newFeishuTestUserRepo(existing)
	server := newFeishuMockServer(t, 0, 0, &feishuUserInfoData{
		OpenID: "ou_newbind", Name: "Corp", EnterpriseEmail: "corp@example.com",
	})
	defer server.Close()

	svc := &userService{
		userRepo:      repo,
		tokenRepo:     &feishuTokenRepo{},
		tenantService: &provisioningTenantService{},
		memberService: &membershipLookupService{byTenant: map[uint64]*types.TenantMember{
			2: {TenantID: 2, Status: types.TenantMemberStatusActive, Role: types.TenantRoleOwner},
		}},
		config: &config.Config{FeishuLogin: &config.FeishuLoginConfig{
			Enable: true, AppID: "id", AppSecret: "secret",
			TokenEndpoint:    server.URL + "/token",
			UserInfoEndpoint: server.URL + "/user_info",
		}},
	}

	resp, err := svc.LoginWithFeishu(context.Background(), "code", "https://app.example/callback", types.TenantProvisioningCreatePersonal)
	if err != nil {
		t.Fatalf("LoginWithFeishu: %v", err)
	}
	if resp.IsNewUser {
		t.Fatalf("expected existing user link")
	}
	stored, _ := repo.GetUserByFeishuOpenID(context.Background(), "ou_newbind")
	if stored == nil || stored.ID != "u2" {
		t.Fatalf("open_id not bound: %+v", stored)
	}
}

func TestLoginWithFeishuEmailConflict(t *testing.T) {
	withFeishuSSRFWhitelist(t)
	existing := &types.User{
		ID: "u3", Email: "taken@example.com", Username: "taken",
		FeishuOpenID: "ou_other", IsActive: true, PasswordHash: "x",
	}
	repo := newFeishuTestUserRepo(existing)
	server := newFeishuMockServer(t, 0, 0, &feishuUserInfoData{
		OpenID: "ou_attacker", Email: "taken@example.com",
	})
	defer server.Close()

	svc := &userService{
		userRepo:  repo,
		tokenRepo: &feishuTokenRepo{},
		config: &config.Config{FeishuLogin: &config.FeishuLoginConfig{
			Enable: true, AppID: "id", AppSecret: "secret",
			TokenEndpoint:    server.URL + "/token",
			UserInfoEndpoint: server.URL + "/user_info",
		}},
	}

	_, err := svc.LoginWithFeishu(context.Background(), "code", "https://app.example/callback", types.TenantProvisioningCreatePersonal)
	if !errors.Is(err, ErrFeishuIdentityConflict) {
		t.Fatalf("error = %v, want ErrFeishuIdentityConflict", err)
	}
}

func TestLoginWithFeishuAutoProvisionNoEmail(t *testing.T) {
	withFeishuSSRFWhitelist(t)
	repo := newFeishuTestUserRepo()
	server := newFeishuMockServer(t, 0, 0, &feishuUserInfoData{
		OpenID: "ou_noreply", Name: "NoMail",
	})
	defer server.Close()

	svc := &userService{
		userRepo:      repo,
		tokenRepo:     &feishuTokenRepo{},
		tenantService: &provisioningTenantService{},
		memberService: &membershipLookupService{},
		config: &config.Config{FeishuLogin: &config.FeishuLoginConfig{
			Enable: true, AppID: "id", AppSecret: "secret",
			TokenEndpoint:    server.URL + "/token",
			UserInfoEndpoint: server.URL + "/user_info",
		}},
	}

	resp, err := svc.LoginWithFeishu(context.Background(), "code", "https://app.example/callback", types.TenantProvisioningTenantless)
	if err != nil {
		t.Fatalf("LoginWithFeishu: %v", err)
	}
	if !resp.IsNewUser {
		t.Fatalf("expected new user")
	}
	if !strings.HasSuffix(resp.User.Email, "@"+feishuNoreplyEmailDomain) {
		t.Fatalf("email = %q", resp.User.Email)
	}
	if resp.User.FeishuOpenID != "ou_noreply" {
		t.Fatalf("open_id = %q", resp.User.FeishuOpenID)
	}
	if repo.createCalls == 0 {
		t.Fatalf("expected CreateUser via Register")
	}
}
