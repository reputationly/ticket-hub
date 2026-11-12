package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/notifier"
	"gorm.io/gorm"
)

func timeZero() time.Time { return time.Time{} }
func randomID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func (s *Server) rules(c *gin.Context) {
	pageResult[model.NotifyRule](c, s.db.WithContext(c.Request.Context()).Model(&model.NotifyRule{}))
}
func (s *Server) getRule(c *gin.Context) {
	r, found := record[model.NotifyRule](s, c)
	if found {
		ok(c, r)
	}
}
func (s *Server) validateRule(c *gin.Context, r model.NotifyRule) bool {
	if _, e := notifier.Render(r.Actions.Template, model.Ticket{}, model.TicketEvent{}, ""); e != nil {
		fail(c, 400, "invalid_request", e.Error())
		return false
	}
	for _, typ := range r.Actions.PersonalChannels {
		if typ != "sms_aliyun" && typ != "sms" && typ != "email" && typ != "inapp" {
			fail(c, 400, "invalid_request", "个人渠道类型错误")
			return false
		}
	}
	for _, id := range r.Actions.UserIDs {
		var n int64
		if e := s.db.WithContext(c.Request.Context()).Model(&model.User{}).Where("id = ? AND enabled = ?", id, true).Count(&n).Error; e != nil {
			internal(c)
			return false
		}
		if n == 0 {
			fail(c, 400, "invalid_request", "目标用户不存在或已停用")
			return false
		}
	}
	for _, id := range r.Actions.GroupChannelIDs {
		var n int64
		if e := s.db.WithContext(c.Request.Context()).Model(&model.Channel{}).Where("id = ? AND enabled = ? AND type IN ?", id, true, []string{"wecom_bot", "feishu_bot"}).Count(&n).Error; e != nil {
			internal(c)
			return false
		}
		if n == 0 {
			fail(c, 400, "invalid_request", "群渠道不存在或未启用")
			return false
		}
	}
	return true
}
func (s *Server) createRule(c *gin.Context) { s.saveRule(c, nil) }
func (s *Server) putRule(c *gin.Context) {
	r, found := record[model.NotifyRule](s, c)
	if found {
		s.saveRule(c, &r)
	}
}
func (s *Server) saveRule(c *gin.Context, old *model.NotifyRule) {
	var req model.NotifyRule
	if !bind(c, &req) || !s.validateRule(c, req) {
		return
	}
	req.ID = 0
	req.CreatedAt = timeZero()
	req.UpdatedAt = timeZero()
	action := "创建通知规则"
	if old != nil {
		req.ID = old.ID
		req.CreatedAt = old.CreatedAt
		action = "更新通知规则"
	}
	if e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Save(&req).Error; e != nil {
			return e
		}
		return audit(tx, c, action, strconv.FormatUint(uint64(req.ID), 10))
	}); e != nil {
		internal(c)
		return
	}
	ok(c, req)
}
func (s *Server) deleteRule(c *gin.Context) {
	r, found := record[model.NotifyRule](s, c)
	if !found {
		return
	}
	if e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Delete(&r).Error; e != nil {
			return e
		}
		return audit(tx, c, "删除通知规则", c.Param("id"))
	}); e != nil {
		internal(c)
		return
	}
	ok(c, nil)
}
func (s *Server) previewTemplate(c *gin.Context) {
	var req struct {
		Template string            `json:"template"`
		Ticket   model.Ticket      `json:"ticket"`
		Event    model.TicketEvent `json:"event"`
	}
	if e := json.NewDecoder(c.Request.Body).Decode(&req); e != nil {
		fail(c, 400, "invalid_request", "请求参数不正确")
		return
	}
	settings, e := s.settings(c.Request.Context())
	if e != nil {
		internal(c)
		return
	}
	text, e := notifier.Render(req.Template, req.Ticket, req.Event, settings.PublicURL)
	if e != nil {
		fail(c, 400, "invalid_request", e.Error())
		return
	}
	ok(c, gin.H{"content": text})
}
func (s *Server) jobs(c *gin.Context) {
	q := s.db.WithContext(c.Request.Context()).Model(&model.NotifyJob{})
	for _, key := range []string{"status", "channel_type", "channel_id", "user_id", "event_id"} {
		if v := c.Query(key); v != "" {
			q = q.Where(key+" = ?", v)
		}
	}
	q, valid := timeRange(c, q, "created_at")
	if valid {
		pageResult[model.NotifyJob](c, q)
	}
}
func (s *Server) resend(c *gin.Context) {
	job, found := record[model.NotifyJob](s, c)
	if !found {
		return
	}
	if s.notify == nil {
		fail(c, 503, "unavailable", "通知服务未启动")
		return
	}
	if job.Status == "processing" {
		fail(c, 409, "conflict", "任务正在处理中")
		return
	}
	e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		r := tx.Model(&model.NotifyJob{}).Where("id = ? AND status <> ?", job.ID, "processing").Updates(map[string]any{"status": "pending", "retries": 0, "last_error": "", "lease_until": nil, "lease_token": "", "next_attempt_at": s.notify.Now()})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return errors.New("任务正在处理")
		}
		return audit(tx, c, "重发通知", c.Param("id"))
	})
	if e != nil {
		internal(c)
		return
	}
	if e = s.notify.Queue.Push(c.Request.Context(), job.ID); e != nil {
		fail(c, 503, "enqueue_failed", "重发任务已保存，等待恢复入队")
		return
	}
	ok(c, nil)
}
