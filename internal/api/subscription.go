package api

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/notifier"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) getSubscription(c *gin.Context) {
	var sub model.Subscription
	e := s.db.WithContext(c.Request.Context()).First(&sub, "user_id = ?", currentUser(c).ID).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		sub = model.Subscription{UserID: currentUser(c).ID, Timezone: "Asia/Shanghai", Channels: []string{}}
	} else if e != nil {
		internal(c)
		return
	}
	var channels []model.Channel
	if e = s.db.WithContext(c.Request.Context()).Where("enabled = ? AND type IN ?", true, []string{"sms_aliyun", "email", "inapp"}).Find(&channels).Error; e != nil {
		internal(c)
		return
	}
	available := []string{}
	seen := map[string]bool{}
	for _, ch := range channels {
		if !seen[ch.Type] {
			available = append(available, ch.Type)
			seen[ch.Type] = true
		}
	}
	ok(c, gin.H{"subscription": sub, "available_channels": available})
}
func (s *Server) putSubscription(c *gin.Context) {
	var sub model.Subscription
	if !bind(c, &sub) {
		return
	}
	sub.UserID = currentUser(c).ID
	sub.UpdatedAt = timeZero()
	if e := notifier.ValidateSubscription(sub); e != nil {
		fail(c, 400, "invalid_request", e.Error())
		return
	}
	for i, typ := range sub.Channels {
		if typ == "sms" {
			typ = "sms_aliyun"
			sub.Channels[i] = typ
		}
		if typ != "sms_aliyun" && typ != "email" && typ != "inapp" {
			fail(c, 400, "invalid_request", "只能选择个人渠道")
			return
		}
		var n int64
		if e := s.db.WithContext(c.Request.Context()).Model(&model.Channel{}).Where("enabled = ? AND type = ?", true, typ).Count(&n).Error; e != nil {
			internal(c)
			return
		}
		if n == 0 {
			fail(c, 400, "invalid_request", "所选渠道未启用")
			return
		}
	}
	if e := s.db.WithContext(c.Request.Context()).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, UpdateAll: true}).Create(&sub).Error; e != nil {
		internal(c)
		return
	}
	ok(c, sub)
}
func (s *Server) putProfile(c *gin.Context) {
	var req struct {
		Phone        string `json:"phone" binding:"max=32"`
		Email        string `json:"email" binding:"omitempty,email,max=254"`
		FeishuOpenID string `json:"feishu_open_id" binding:"max=128"`
	}
	if !bind(c, &req) {
		return
	}
	req.Phone = strings.TrimSpace(req.Phone)
	for i, r := range req.Phone {
		if r < '0' || r > '9' {
			if i != 0 || r != '+' {
				fail(c, 400, "invalid_request", "手机号只允许数字和前导加号")
				return
			}
		}
	}
	u := currentUser(c)
	if e := s.db.WithContext(c.Request.Context()).Model(&model.User{}).Where("id = ?", u.ID).Updates(map[string]any{"phone": req.Phone, "email": req.Email, "feishu_open_id": req.FeishuOpenID}).Error; e != nil {
		internal(c)
		return
	}
	u.Phone, u.Email, u.FeishuOpenID = req.Phone, req.Email, req.FeishuOpenID
	ok(c, u)
}
