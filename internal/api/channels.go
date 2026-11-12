package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/sender"
	"gorm.io/gorm"
)

func (s *Server) channels(c *gin.Context) {
	var rows []model.Channel
	if e := s.db.WithContext(c.Request.Context()).Order("id DESC").Find(&rows).Error; e != nil {
		internal(c)
		return
	}
	for i := range rows {
		rows[i] = sender.Masked(rows[i])
	}
	ok(c, gin.H{"items": rows})
}
func (s *Server) getChannel(c *gin.Context) {
	ch, found := record[model.Channel](s, c)
	if found {
		ok(c, sender.Masked(ch))
	}
}
func (s *Server) createChannel(c *gin.Context) { s.saveChannel(c, nil) }
func (s *Server) putChannel(c *gin.Context) {
	ch, found := record[model.Channel](s, c)
	if found {
		s.saveChannel(c, &ch)
	}
}
func (s *Server) saveChannel(c *gin.Context, old *model.Channel) {
	var req model.Channel
	if !bind(c, &req) {
		return
	}
	cipher, e := crypto.New(s.config.Secret)
	if e != nil {
		internal(c)
		return
	}
	req, e = sender.Prepare(cipher, req, old)
	if e != nil {
		fail(c, 400, "invalid_request", e.Error())
		return
	}
	req.ID = 0
	req.CreatedAt = timeZero()
	req.UpdatedAt = timeZero()
	action := "创建通知渠道"
	if old != nil {
		req.ID = old.ID
		req.CreatedAt = old.CreatedAt
		action = "更新通知渠道"
	}
	e = s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Save(&req).Error; e != nil {
			return e
		}
		return audit(tx, c, action, strconv.FormatUint(uint64(req.ID), 10))
	})
	if e != nil {
		internal(c)
		return
	}
	ok(c, sender.Masked(req))
}
func (s *Server) deleteChannel(c *gin.Context) {
	ch, found := record[model.Channel](s, c)
	if !found {
		return
	}
	e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Delete(&ch).Error; e != nil {
			return e
		}
		return audit(tx, c, "删除通知渠道", c.Param("id"))
	})
	if e != nil {
		internal(c)
		return
	}
	ok(c, nil)
}
func (s *Server) testChannel(c *gin.Context) {
	ch, found := record[model.Channel](s, c)
	if !found {
		return
	}
	if s.notify == nil {
		fail(c, 503, "unavailable", "通知服务未启动")
		return
	}
	if !ch.Enabled {
		fail(c, 400, "invalid_request", "渠道未启用")
		return
	}
	var req struct {
		UserID         uint     `json:"user_id"`
		Phone          string   `json:"phone" binding:"max=32"`
		Email          string   `json:"email" binding:"omitempty,email,max=254"`
		Title          string   `json:"title" binding:"max=500"`
		Content        string   `json:"content" binding:"max=16000"`
		MentionMobiles []string `json:"mentioned_mobile_list"`
		MentionOpenIDs []string `json:"mention_open_ids"`
	}
	if !bind(c, &req) {
		return
	}
	d := model.Delivery{UserID: req.UserID, Phone: req.Phone, Email: req.Email, Title: req.Title, Content: req.Content, MentionMobiles: req.MentionMobiles, MentionOpenIDs: req.MentionOpenIDs}
	if d.Title == "" {
		d.Title = "工单中心测试通知"
	}
	if d.Content == "" {
		d.Content = "这是一条测试提醒。"
	}
	if req.UserID != 0 {
		var u model.User
		if e := s.db.WithContext(c.Request.Context()).First(&u, req.UserID).Error; e != nil {
			fail(c, 400, "invalid_request", "目标用户不存在")
			return
		}
		if d.Phone == "" {
			d.Phone = u.Phone
		}
		if d.Email == "" {
			d.Email = u.Email
		}
	}
	if ch.Type == "inapp" && d.UserID == 0 || ch.Type == "sms_aliyun" && d.Phone == "" || ch.Type == "email" && d.Email == "" {
		fail(c, 400, "invalid_request", "缺少测试目标")
		return
	}
	d.Variables = map[string]string{"title": d.Title, "status": "测试", "site": "测试机房", "kind": "test", "link": "", "upstream_id": "TEST", "node": "测试", "assignee": "测试", "priority": "normal", "event": "ticket.created"}
	job := model.NotifyJob{DedupeKey: "test:" + randomID(), UserID: d.UserID, ChannelID: ch.ID, ChannelType: ch.Type, Delivery: d, Status: "pending", NextAttemptAt: s.notify.Now()}
	e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&job).Error; e != nil {
			return e
		}
		return audit(tx, c, "测试通知渠道", c.Param("id"))
	})
	if e != nil {
		internal(c)
		return
	}
	if e = s.notify.Queue.Push(c.Request.Context(), job.ID); e != nil {
		fail(c, 503, "enqueue_failed", "测试任务已保存，等待恢复入队")
		return
	}
	ok(c, job)
}
