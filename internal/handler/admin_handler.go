package handler

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/middleware"
	"gorm.io/gorm"
)

// 默认管理员账号（可用环境变量覆盖：MXGT_ADMIN_USER / MXGT_ADMIN_PASSWORD）
var (
	adminUser     = "admin"
	adminPassword = "admin123"
)

func init() {
	if u := os.Getenv("MXGT_ADMIN_USER"); u != "" {
		adminUser = u
	}
	if p := os.Getenv("MXGT_ADMIN_PASSWORD"); p != "" {
		adminPassword = p
	}
}

// AdminHandler 管理后台处理器（登录 + 解析规则多规则 CRUD）
type AdminHandler struct {
	db *gorm.DB
}

// NewAdminHandler 创建管理后台处理器
func NewAdminHandler(db *gorm.DB) *AdminHandler {
	return &AdminHandler{db: db}
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login 处理 POST /admin/login
func (h *AdminHandler) Login(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"code": 0, "msg": "参数错误"})
	}
	if req.Username != adminUser || req.Password != adminPassword {
		return c.JSON(http.StatusUnauthorized, map[string]any{"code": 0, "msg": "账号或密码错误"})
	}
	token, err := middleware.GenerateToken(req.Username)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"code": 0, "msg": "生成令牌失败"})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"code": 1, "msg": "ok",
		"data": map[string]any{"token": token, "username": req.Username},
	})
}

// ok 统一成功响应
func ok(c echo.Context, data any) error {
	return c.JSON(http.StatusOK, map[string]any{"code": 1, "msg": "ok", "data": data})
}

// fail 统一失败响应
func fail(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{"code": 0, "msg": msg})
}
