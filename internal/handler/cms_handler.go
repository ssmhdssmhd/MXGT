package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/ssmhdssmhd/MXGT/internal/cms"
	"github.com/ssmhdssmhd/MXGT/internal/models"
	"gorm.io/gorm"
)

// CMSHandler 苹果 CMS v10 对外输出（/api.php/provide/vod/）
type CMSHandler struct {
	db *gorm.DB
}

// NewCMSHandler 创建 CMS 处理器
func NewCMSHandler(db *gorm.DB) *CMSHandler {
	return &CMSHandler{db: db}
}

const cmsPageSize = 20

// Provide 统一入口：ac=list / detail / search / play
func (h *CMSHandler) Provide(c echo.Context) error {
	switch c.QueryParam("ac") {
	case "detail":
		return h.detail(c)
	case "search":
		return h.search(c)
	case "play":
		return h.play(c)
	case "list", "":
		return h.list(c)
	default:
		return h.list(c)
	}
}

// list 分类列表（支持 ?pg=页数&t=分类）
func (h *CMSHandler) list(c echo.Context) error {
	pg := atoi(c.QueryParam("pg"), 1)
	if pg < 1 {
		pg = 1
	}
	t := c.QueryParam("t")

	q := h.db.Model(&models.Vod{})
	if t != "" {
		q = q.Where("category = ?", t)
	}
	var total int64
	q.Count(&total)

	var vods []models.Vod
	q.Preload("Episodes", func(db *gorm.DB) *gorm.DB {
		return db.Order("episode_no ASC")
	}).
		Order("id DESC").
		Offset((pg - 1) * cmsPageSize).
		Limit(cmsPageSize).
		Find(&vods)

	return c.JSON(http.StatusOK, h.buildList(vods, pg, int(total)))
}

// detail 详情（?ids=1,2,3）
func (h *CMSHandler) detail(c echo.Context) error {
	ids := parseIDs(c.QueryParam("ids"))
	if len(ids) == 0 {
		return c.JSON(http.StatusOK, h.emptyList(1))
	}
	var vods []models.Vod
	h.db.Preload("Episodes", func(db *gorm.DB) *gorm.DB {
		return db.Order("episode_no ASC")
	}).
		Where("id IN ?", ids).
		Find(&vods)
	return c.JSON(http.StatusOK, h.buildList(vods, 1, len(vods)))
}

// search 搜索（?wd=关键词）
func (h *CMSHandler) search(c echo.Context) error {
	wd := strings.TrimSpace(c.QueryParam("wd"))
	if wd == "" {
		return c.JSON(http.StatusOK, h.emptyList(1))
	}
	var vods []models.Vod
	h.db.Preload("Episodes", func(db *gorm.DB) *gorm.DB {
		return db.Order("episode_no ASC")
	}).
		Where("name LIKE ?", "%"+wd+"%").
		Order("id DESC").
		Limit(cmsPageSize).
		Find(&vods)
	return c.JSON(http.StatusOK, h.buildList(vods, 1, len(vods)))
}

// play 直接返回某集真实播放链接（?id=1&ep=5）
func (h *CMSHandler) play(c echo.Context) error {
	id := atoi(c.QueryParam("id"), 0)
	ep := atoi(c.QueryParam("ep"), 0)
	if id <= 0 {
		return c.JSON(http.StatusOK, cms.PlayResponse{Code: 0, Msg: "缺少 id"})
	}

	// 优先精确匹配某集的真实 URL
	var episode models.Episode
	err := h.db.Where("vod_id = ? AND episode_no = ?", id, ep).First(&episode).Error
	if err == nil && episode.SourceURL != "" {
		return c.JSON(http.StatusOK, cms.PlayResponse{Code: 1, Msg: "播放地址", URL: episode.SourceURL})
	}

	// 兜底：返回该影片完整播放串（由采集端自行解析）
	var vod models.Vod
	if err := h.db.Preload("Episodes", func(db *gorm.DB) *gorm.DB {
		return db.Order("episode_no ASC")
	}).First(&vod, id).Error; err != nil {
		return c.JSON(http.StatusOK, cms.PlayResponse{Code: 0, Msg: "影片不存在"})
	}
	return c.JSON(http.StatusOK, cms.PlayResponse{Code: 1, Msg: "播放地址", URL: cms.ToCMSVod(&vod).VodPlayURL})
}

// buildList 组装列表响应
func (h *CMSHandler) buildList(vods []models.Vod, pg, total int) *cms.ListResponse {
	list := make([]cms.CMSVod, 0, len(vods))
	for i := range vods {
		list = append(list, cms.ToCMSVod(&vods[i]))
	}
	pageCount := (total + cmsPageSize - 1) / cmsPageSize
	if pageCount < 1 {
		pageCount = 1
	}
	return &cms.ListResponse{
		Code:      1,
		Msg:       "数据列表",
		Page:      pg,
		PageCount: pageCount,
		Limit:     strconv.Itoa(cmsPageSize),
		Total:     total,
		List:      list,
	}
}

func (h *CMSHandler) emptyList(pg int) *cms.ListResponse {
	return &cms.ListResponse{
		Code: 1, Msg: "数据列表", Page: pg, PageCount: 1,
		Limit: strconv.Itoa(cmsPageSize), Total: 0, List: []cms.CMSVod{},
	}
}

// parseIDs 解析 "1,2,3" → []uint
func parseIDs(s string) []uint {
	var ids []uint
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.ParseUint(strings.TrimSpace(p), 10, 64); err == nil && n > 0 {
			ids = append(ids, uint(n))
		}
	}
	return ids
}

// atoi 安全字符串转 int
func atoi(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
