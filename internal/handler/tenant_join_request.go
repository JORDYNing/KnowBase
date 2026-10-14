package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type TenantJoinRequestHandler struct {
	service interfaces.TenantJoinRequestService
	users   interfaces.UserService
}

func NewTenantJoinRequestHandler(service interfaces.TenantJoinRequestService, users interfaces.UserService) *TenantJoinRequestHandler {
	return &TenantJoinRequestHandler{service: service, users: users}
}
func joinRequestError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, types.ErrJoinTargetUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "TEAM_WORKSPACE_UNAVAILABLE", "error": err.Error()})
	case errors.Is(err, types.ErrJoinRequestNotFound):
		c.Error(apperrors.NewNotFoundError(err.Error()))
	case errors.Is(err, types.ErrJoinRequestConflict):
		c.Error(apperrors.NewConflictError(err.Error()))
	case errors.Is(err, types.ErrJoinRequestForbidden):
		c.Error(apperrors.NewForbiddenError(err.Error()))
	case errors.Is(err, types.ErrJoinRoleInvalid):
		c.Error(apperrors.NewValidationError(err.Error()))
	default:
		c.Error(apperrors.NewInternalServerError("Failed to process workspace application"))
	}
}

// GetMyRequest godoc
// @Summary 查看本人加入团队知识空间的申请
// @Description 返回固定空间10000的申请、可用性与当前成员资格；已登录但无空间的用户也可访问。
// @Tags 空间申请
// @Produce json
// @Success 200 {object} map[string]interface{} "data is types.TenantJoinRequestState"
// @Failure 401 {object} apperrors.AppError
// @Security Bearer
// @Router /me/tenant-join-request [get]
func (h *TenantJoinRequestHandler) GetMyRequest(c *gin.Context) {
	user, err := h.users.GetCurrentUser(c.Request.Context())
	if err != nil || user == nil {
		c.Error(apperrors.NewUnauthorizedError("authentication required"))
		return
	}
	state, err := h.service.GetState(c.Request.Context(), user.ID)
	if err != nil {
		joinRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": state})
}

// SubmitTenantJoinRequestRequest is optional for initial submission and pending
// retries. Resubmitting a rejected request requires its current version.
type SubmitTenantJoinRequestRequest struct {
	Version uint64 `json:"version"`
}

// ReviewTenantJoinRequestRequest requires an explicit role on approval.
// Owners may assign viewer/contributor/admin; Admins viewer/contributor only.
type ReviewTenantJoinRequestRequest struct {
	Approved *bool            `json:"approved" binding:"required"`
	Role     types.TenantRole `json:"role" enums:"viewer,contributor,admin"`
	Message  string           `json:"message" binding:"max=500"`
	Version  uint64           `json:"version" binding:"required"`
}

// SubmitMyRequest godoc
// @Summary 提交或重新提交本人空间申请
// @Description 首次提交与pending重复提交幂等；被拒后需携带当前version手动重申，获批或bootstrap申请不能重申。
// @Tags 空间申请
// @Accept json
// @Produce json
// @Param request body SubmitTenantJoinRequestRequest false "重申时提供当前version"
// @Success 200 {object} map[string]interface{} "data is types.TenantJoinRequestState"
// @Failure 403 {object} apperrors.AppError
// @Failure 409 {object} apperrors.AppError
// @Failure 503 {object} apperrors.AppError
// @Security Bearer
// @Router /me/tenant-join-request [post]
func (h *TenantJoinRequestHandler) SubmitMyRequest(c *gin.Context) {
	user, err := h.users.GetCurrentUser(c.Request.Context())
	if err != nil || user == nil {
		c.Error(apperrors.NewUnauthorizedError("authentication required"))
		return
	}
	var req SubmitTenantJoinRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.Error(apperrors.NewValidationError("Invalid application parameters"))
		return
	}
	state, err := h.service.Submit(c.Request.Context(), user.ID, req.Version)
	if err != nil {
		joinRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": state})
}

// ListRequests godoc
// @Summary 分页查看团队知识空间申请
// @Description 仅当前知识空间的Owner或Admin可查看，按更新时间倒序返回申请与申请人资料。
// @Tags 空间申请
// @Produce json
// @Param id path int true "知识空间ID"
// @Param status query string false "留空查看全部" Enums(pending,approved,rejected) default(pending)
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页条数，最大100" default(20)
// @Success 200 {object} map[string]interface{} "data contains requests ([]types.TenantJoinRequestResponse),total,page,page_size"
// @Failure 403 {object} apperrors.AppError
// @Security Bearer
// @Router /tenants/{id}/join-requests [get]
func (h *TenantJoinRequestHandler) ListRequests(c *gin.Context) {
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}
	page, pageSize, ok := parseListPagination(c)
	if !ok {
		return
	}
	status := strings.TrimSpace(c.DefaultQuery("status", "pending"))
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		c.Error(apperrors.NewValidationError("Invalid application status"))
		return
	}
	rows, total, err := h.service.List(c.Request.Context(), tenantID, status, page, pageSize)
	if err != nil {
		joinRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"requests": rows, "total": total, "page": page, "page_size": pageSize}})
}

// ReviewRequest godoc
// @Summary 审批知识空间加入申请
// @Description Owner可授予viewer/contributor/admin，Admin可授予viewer/contributor；批准时必须选择角色。version防止并发覆盖，相同决定重试幂等。
// @Tags 空间申请
// @Accept json
// @Produce json
// @Param id path int true "知识空间ID"
// @Param request_id path int true "申请ID"
// @Param request body ReviewTenantJoinRequestRequest true "审批决定与原申请version"
// @Success 200 {object} map[string]interface{} "data is types.TenantJoinRequest"
// @Failure 400 {object} apperrors.AppError
// @Failure 403 {object} apperrors.AppError
// @Failure 404 {object} apperrors.AppError
// @Failure 409 {object} apperrors.AppError
// @Security Bearer
// @Router /tenants/{id}/join-requests/{request_id}/review [put]
func (h *TenantJoinRequestHandler) ReviewRequest(c *gin.Context) {
	tenantID, ok := parseTenantIDFromPath(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("request_id"), 10, 64)
	if err != nil || id == 0 {
		c.Error(apperrors.NewValidationError("Invalid application ID"))
		return
	}
	var req ReviewTenantJoinRequestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(apperrors.NewValidationError("Invalid review parameters").WithDetails(err.Error()))
		return
	}
	user, err := h.users.GetCurrentUser(c.Request.Context())
	if err != nil || user == nil {
		c.Error(apperrors.NewUnauthorizedError("authentication required"))
		return
	}
	row, err := h.service.Review(c.Request.Context(), tenantID, id, req.Version, *req.Approved, req.Role, user.ID, req.Message)
	if err != nil {
		joinRequestError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
