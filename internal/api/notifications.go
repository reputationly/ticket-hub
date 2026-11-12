package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/sender"
)

func (s *Server) notificationRoutes(secured, admin *gin.RouterGroup) {
	secured.GET("/tickets", s.listTickets)
	secured.GET("/tickets/:id", s.getTicket)
	if s.config.DevMode {
		secured.POST("/dev/tickets", func(c *gin.Context) {
			if currentUser(c).Role != "admin" {
				fail(c, 403, "forbidden", "仅管理员可注入工单")
				return
			}
			s.injectTicket(c)
		})
	}
	secured.GET("/notifications", s.notifications)
	secured.GET("/notifications/unread-count", s.unreadCount)
	secured.PUT("/notifications/:id/read", s.markRead)
	secured.PUT("/notifications/read-all", s.readAll)
	secured.GET("/notifications/stream", s.notificationStream)
	secured.GET("/me/subscription", s.getSubscription)
	secured.PUT("/me/subscription", s.putSubscription)
	secured.PUT("/me/profile", s.putProfile)
	admin.GET("/channels", s.channels)
	admin.POST("/channels", s.createChannel)
	admin.GET("/channels/:id", s.getChannel)
	admin.PUT("/channels/:id", s.putChannel)
	admin.DELETE("/channels/:id", s.deleteChannel)
	admin.POST("/channels/:id/test", s.testChannel)
	admin.GET("/rules", s.rules)
	admin.POST("/rules", s.createRule)
	admin.GET("/rules/:id", s.getRule)
	admin.PUT("/rules/:id", s.putRule)
	admin.DELETE("/rules/:id", s.deleteRule)
	admin.POST("/templates/preview", s.previewTemplate)
	admin.GET("/notify-jobs", s.jobs)
	admin.POST("/notify-jobs/:id/resend", s.resend)
}
func (s *Server) notifications(c *gin.Context) {
	q := s.db.WithContext(c.Request.Context()).Model(&model.Notification{}).Where("user_id = ?", currentUser(c).ID)
	if read := c.Query("read"); read != "" {
		if read != "true" && read != "false" {
			fail(c, 400, "invalid_request", "已读状态错误")
			return
		}
		q = q.Where("read = ?", read == "true")
	}
	pageResult[model.Notification](c, q)
}
func (s *Server) unreadCount(c *gin.Context) {
	var count int64
	if e := s.db.WithContext(c.Request.Context()).Model(&model.Notification{}).Where("user_id = ? AND read = ?", currentUser(c).ID, false).Count(&count).Error; e != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"count": count})
}
func (s *Server) markRead(c *gin.Context) {
	n, found := record[model.Notification](s, c)
	if !found {
		return
	}
	if n.UserID != currentUser(c).ID {
		fail(c, 404, "not_found", "记录不存在")
		return
	}
	if e := s.db.WithContext(c.Request.Context()).Model(&n).Update("read", true).Error; e != nil {
		internal(c)
		return
	}
	ok(c, nil)
}
func (s *Server) readAll(c *gin.Context) {
	if e := s.db.WithContext(c.Request.Context()).Model(&model.Notification{}).Where("user_id = ?", currentUser(c).ID).Update("read", true).Error; e != nil {
		internal(c)
		return
	}
	ok(c, nil)
}
func (s *Server) notificationStream(c *gin.Context) {
	sub, e := s.cache.Subscribe(c.Request.Context(), sender.Topic(currentUser(c).ID))
	if e != nil {
		fail(c, 503, "unavailable", "消息订阅失败")
		return
	}
	defer sub.Close()
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	rc := http.NewResponseController(c.Writer)
	_ = rc.SetWriteDeadline(time.Time{})
	_, _ = fmt.Fprint(c.Writer, ": 已连接\n\n")
	c.Writer.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case msg, open := <-sub.Messages():
			if !open {
				return
			}
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, e = fmt.Fprintf(c.Writer, "event: notification\ndata: %s\n\n", msg); e != nil {
				return
			}
			c.Writer.Flush()
		case <-ticker.C:
			claims := currentClaims(c)
			if time.Now().After(claims.ExpiresAt.Time) {
				return
			}
			if _, e = s.cache.Get(c.Request.Context(), "blacklist:"+claims.ID); !errors.Is(e, cache.ErrMiss) {
				return
			}
			var user model.User
			if e = s.db.WithContext(c.Request.Context()).First(&user, currentUser(c).ID).Error; e != nil || !user.Enabled || user.AuthVersion != currentUser(c).AuthVersion {
				return
			}
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, e = fmt.Fprint(c.Writer, ": 心跳\n\n"); e != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}
