// Package middleware 通用中间件（JWT 鉴权）
package middleware

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

// jwtSecret 签名密钥（可用环境变量 MXGT_JWT_SECRET 覆盖）
var jwtSecret = func() []byte {
	if s := os.Getenv("MXGT_JWT_SECRET"); s != "" {
		return []byte(s)
	}
	return []byte("mxgt-default-secret-change-me")
}()

// Claims JWT 声明
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken 生成管理后台登录令牌（有效期 12h）
func GenerateToken(username string) (string, error) {
	claims := Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(12 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "mxgt",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// JWTAuth JWT 鉴权中间件：校验 Authorization: Bearer <token>
func JWTAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		auth := c.Request().Header.Get(echo.HeaderAuthorization)
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			return c.JSON(http.StatusUnauthorized, map[string]any{
				"code": 0, "msg": "未登录或令牌缺失",
			})
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			return c.JSON(http.StatusUnauthorized, map[string]any{
				"code": 0, "msg": "令牌无效或已过期",
			})
		}
		c.Set("username", claims.Username)
		return next(c)
	}
}
