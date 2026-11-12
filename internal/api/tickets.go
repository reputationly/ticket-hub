package api

import (
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/model"
	"gorm.io/gorm"
)

func record[T any](s *Server, c *gin.Context) (T, bool) {
	var row T
	id, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || id == 0 {
		fail(c, 400, "invalid_request", "编号格式错误")
		return row, false
	}
	e = s.db.WithContext(c.Request.Context()).First(&row, id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		fail(c, 404, "not_found", "记录不存在")
		return row, false
	}
	if e != nil {
		internal(c)
		return row, false
	}
	return row, true
}
func timeRange(c *gin.Context, q *gorm.DB, column string) (*gorm.DB, bool) {
	for _, f := range []struct{ key, op string }{{"from", ">="}, {"to", "<="}} {
		if raw := c.Query(f.key); raw != "" {
			value, e := time.Parse(time.RFC3339, raw)
			if e != nil {
				fail(c, 400, "invalid_request", "时间范围须使用 RFC3339 格式")
				return q, false
			}
			q = q.Where(column+" "+f.op+" ?", value)
		}
	}
	return q, true
}
func pageResult[T any](c *gin.Context, q *gorm.DB) {
	page, size := pagination(c)
	var rows []T
	var count int64
	if e := q.Count(&count).Error; e != nil {
		internal(c)
		return
	}
	if e := q.Order("id DESC").Limit(size).Offset((page - 1) * size).Find(&rows).Error; e != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"items": rows, "total": count, "page": page, "page_size": size})
}
func (s *Server) listTickets(c *gin.Context) {
	q := s.db.WithContext(c.Request.Context()).Model(&model.Ticket{})
	for _, key := range []string{"kind", "status", "site"} {
		if v := c.Query(key); v != "" {
			q = q.Where(key+" = ?", v)
		}
	}
	keyword := c.Query("keyword")
	if keyword != "" {
		q = q.Where("LOWER(title) LIKE LOWER(?) OR LOWER(upstream_id) LIKE LOWER(?)", "%"+keyword+"%", "%"+keyword+"%")
	}
	q, valid := timeRange(c, q, "created_at")
	if !valid {
		return
	}
	pageResult[model.Ticket](c, q)
}
func (s *Server) getTicket(c *gin.Context) {
	t, found := record[model.Ticket](s, c)
	if !found {
		return
	}
	events := []model.TicketEvent{}
	if e := s.db.WithContext(c.Request.Context()).Where("ticket_id = ?", t.ID).Order("id ASC").Find(&events).Error; e != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"ticket": t, "events": events})
}
func (s *Server) injectTicket(c *gin.Context) {
	var req model.Ticket
	if !bind(c, &req) {
		return
	}
	t, event, e := s.tickets.Upsert(c.Request.Context(), req)
	if e != nil {
		fail(c, 503, "upsert_failed", "工单写入或通知投递失败，已落库事件会自动恢复")
		return
	}
	ok(c, gin.H{"ticket": t, "event": event})
}
