package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// stubRegisterUserService is a UserService whose ONLY useful method is
// Register; every other call panics. Using an interface embedding plus a
// targeted override keeps the test focused on the Register handler's
// branching logic without dragging in the entire user service surface.
type stubRegisterUserService struct {
	interfaces.UserService
	register func(ctx context.Context, req *types.RegisterRequest) (*types.User, error)
}

func (s *stubRegisterUserService) Register(ctx context.Context, req *types.RegisterRequest) (*types.User, error) {
	return s.register(ctx, req)
}

// errorCapture mirrors gin's default ErrorHandler behaviour for tests:
// when a handler calls c.Error(), we surface it as an HTTP response so the
// recorder reflects the real client-visible status. The production
// middleware does the same thing in middleware/error_handler.go.
func errorCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 {
			return
		}
		err := c.Errors.Last().Err
		if appErr, ok := err.(*apperrors.AppError); ok {
			c.JSON(appErr.HTTPCode, gin.H{"error": appErr.Message})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func newRegisterTestRouter(h *AuthHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(errorCapture())
	r.POST("/auth/register", h.Register)
	return r
}

func doRegister(t *testing.T, r *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// validRegisterBody returns a payload that passes parameter validation, so
// each test is exercising the gate logic and not the body parser.
func validRegisterBody() map[string]string {
	return map[string]string{
		"username": "alice",
		"email":    "alice@example.com",
		"password": "supersecret1",
	}
}

func TestRegisterAlwaysRejectsLegacyRegistrationModes(t *testing.T) {
	for _, mode := range []string{"self_serve", "invite_register", "invite_only", ""} {
		t.Run(mode, func(t *testing.T) {
			called := false
			users := &stubRegisterUserService{register: func(context.Context, *types.RegisterRequest) (*types.User, error) { called = true; return nil, nil }}
			h := NewAuthHandler(&config.Config{Auth: &config.AuthConfig{RegistrationMode: mode}}, users, nil, nil, nil)
			w := doRegister(t, newRegisterTestRouter(h), validRegisterBody())
			if w.Code != http.StatusForbidden || called {
				t.Fatalf("legacy mode %q reopened registration: status=%d service_called=%v", mode, w.Code, called)
			}
		})
	}
}
func TestRegisterNilAuthConfigFailsClosed(t *testing.T) {
	h := NewAuthHandler(&config.Config{}, nil, nil, nil, nil)
	w := doRegister(t, newRegisterTestRouter(h), validRegisterBody())
	if w.Code != http.StatusForbidden {
		t.Fatalf("nil auth config status=%d", w.Code)
	}
}
