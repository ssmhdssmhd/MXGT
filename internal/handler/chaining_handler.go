package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/chaining"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// ChainingHandler 调用 Pipeline 处理器（M13）：chain_nodes CRUD + 排序 + 链路测试
type ChainingHandler struct {
	db *gorm.DB
}

// NewChainingHandler 创建调用 Pipeline 处理器
func NewChainingHandler(db *gorm.DB) *ChainingHandler {
	return &ChainingHandler{db: db}
}

// List 处理 GET /admin/chain/nodes（按 order 升序）
func (h *ChainingHandler) List(c echo.Context) error {
	var list []models.ChainNode
	if err := h.db.Order("sort_order ASC, id ASC").Find(&list).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	// 输出时填充 headers 字段
	for i := range list {
		if list[i].HeadersJSON != "" {
			_ = json.Unmarshal([]byte(list[i].HeadersJSON), &list[i].Headers)
		} else {
			list[i].Headers = json.RawMessage("{}")
		}
	}
	return ok(c, list)
}

// Create 处理 POST /admin/chain/nodes
func (h *ChainingHandler) Create(c echo.Context) error {
	var n models.ChainNode
	if err := c.Bind(&n); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	if n.Name == "" || n.NodeType == "" {
		return fail(c, http.StatusBadRequest, "name / node_type 必填")
	}
	if n.Method == "" {
		n.Method = "GET"
	}
	if n.Fallback == "" {
		n.Fallback = chaining.FallbackSkip
	}
	if n.Enabled == 0 {
		n.Enabled = 1
	}
	if len(n.Headers) > 0 {
		n.HeadersJSON = string(n.Headers)
	}
	if err := h.db.Create(&n).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "创建失败: "+err.Error())
	}
	n.Headers = json.RawMessage(n.HeadersJSON)
	if n.HeadersJSON == "" {
		n.Headers = json.RawMessage("{}")
	}
	return ok(c, n)
}

// Update 处理 PUT /admin/chain/nodes/:id
func (h *ChainingHandler) Update(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "非法 id")
	}
	var n models.ChainNode
	if err := h.db.First(&n, id).Error; err != nil {
		return fail(c, http.StatusNotFound, "节点不存在")
	}
	var req models.ChainNode
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	// 字段级别覆盖
	if req.Name != "" {
		n.Name = req.Name
	}
	if req.NodeType != "" {
		n.NodeType = req.NodeType
	}
	if req.Endpoint != "" {
		n.Endpoint = req.Endpoint
	}
	if req.Method != "" {
		n.Method = req.Method
	}
	if req.ResultPath != "" {
		n.ResultPath = req.ResultPath
	}
	if req.Fallback != "" {
		n.Fallback = req.Fallback
	}
	if req.FallbackTo != "" {
		n.FallbackTo = req.FallbackTo
	}
	if req.Order != 0 {
		n.Order = req.Order
	}
	if req.Enabled != 0 {
		n.Enabled = req.Enabled
	}
	if len(req.Headers) > 0 {
		n.HeadersJSON = string(req.Headers)
	}
	if err := h.db.Save(&n).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "更新失败: "+err.Error())
	}
	n.Headers = json.RawMessage(n.HeadersJSON)
	if n.HeadersJSON == "" {
		n.Headers = json.RawMessage("{}")
	}
	return ok(c, n)
}

// Delete 处理 DELETE /admin/chain/nodes/:id
func (h *ChainingHandler) Delete(c echo.Context) error {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, "非法 id")
	}
	var n models.ChainNode
	if err := h.db.First(&n, id).Error; err != nil {
		return fail(c, http.StatusNotFound, "节点不存在")
	}
	if n.IsBuiltin == 1 {
		return fail(c, http.StatusForbidden, "内置预置节点不可删除")
	}
	if err := h.db.Delete(&models.ChainNode{}, id).Error; err != nil {
		return fail(c, http.StatusInternalServerError, "删除失败: "+err.Error())
	}
	return ok(c, map[string]any{"deleted": id})
}

// Reorder 处理 PUT /admin/chain/reorder（批量调整顺序）
func (h *ChainingHandler) Reorder(c echo.Context) error {
	var req []struct {
		ID    uint `json:"id"`
		Order int  `json:"order"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, http.StatusBadRequest, "参数格式错误")
	}
	for _, item := range req {
		if item.ID == 0 {
			continue
		}
		if err := h.db.Model(&models.ChainNode{}).Where("id = ?", item.ID).
			Update("sort_order", item.Order).Error; err != nil {
			return fail(c, http.StatusInternalServerError, "更新顺序失败: "+err.Error())
		}
	}
	return ok(c, map[string]any{"updated": len(req)})
}

// Test 处理 POST /admin/chain/test（测试整条链路中间结果）
func (h *ChainingHandler) Test(c echo.Context) error {
	var req struct {
		Input string `json:"input"`
	}
	if err := c.Bind(&req); err != nil || req.Input == "" {
		return fail(c, http.StatusBadRequest, "input 必填")
	}
	var nodes []models.ChainNode
	if err := h.db.Where("enabled = ?", 1).Order("sort_order ASC, id ASC").Find(&nodes).Error; err != nil {
		return fail(c, http.StatusInternalServerError, err.Error())
	}
	run := make([]chaining.Node, 0, len(nodes))
	for _, n := range nodes {
		hdrs := map[string]string{}
		if n.HeadersJSON != "" {
			_ = json.Unmarshal([]byte(n.HeadersJSON), &hdrs)
		}
		run = append(run, chaining.Node{
			ID: n.ID, Name: n.Name, Type: n.NodeType,
			Endpoint: n.Endpoint, Method: n.Method, Headers: hdrs,
			ResultPath: n.ResultPath, Fallback: n.Fallback, FallbackTo: n.FallbackTo, Order: n.Order,
		})
	}
	res := chaining.New().Execute(context.Background(), req.Input, run)
	return ok(c, res)
}