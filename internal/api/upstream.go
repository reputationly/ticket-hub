package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/collector"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/mailwatch"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/sender"
	"github.com/reputationly/ticket-hub/internal/syncer"
	"github.com/reputationly/ticket-hub/internal/upstream"
	"gorm.io/gorm"
)

func (s *Server) upstreamRoutes(admin *gin.RouterGroup) {
	admin.GET("/mail-accounts", s.mailAccounts)
	admin.POST("/mail-accounts", s.createMailAccount)
	admin.GET("/mail-accounts/:id", s.getMailAccount)
	admin.PUT("/mail-accounts/:id", s.putMailAccount)
	admin.DELETE("/mail-accounts/:id", s.deleteMailAccount)
	admin.POST("/mail-accounts/:id/test", s.testMailAccount)
	admin.GET("/upstream", s.upstreamAccounts)
	admin.POST("/upstream", s.createUpstream)
	admin.GET("/upstream/:id", s.getUpstream)
	admin.PUT("/upstream/:id", s.putUpstream)
	admin.DELETE("/upstream/:id", s.deleteUpstream)
	admin.POST("/upstream/:id/login", s.loginUpstream)
	admin.POST("/upstream/:id/manual-code", s.manualCode)
	admin.GET("/upstream/:id/status", s.getUpstream)
	admin.GET("/collectors", s.collectors)
	admin.PUT("/collectors/:kind", s.putCollector)
	admin.POST("/collectors/:kind/sync", s.syncCollector)
	admin.GET("/mail-rules", s.mailRules)
	admin.POST("/mail-rules", s.createMailRule)
	admin.GET("/mail-rules/:id", s.getMailRule)
	admin.PUT("/mail-rules/:id", s.putMailRule)
	admin.DELETE("/mail-rules/:id", s.deleteMailRule)
	admin.GET("/sync-logs", s.syncLogs)
}
func hideMail(a model.MailAccount) model.MailAccount {
	if a.Password != "" {
		a.Password = sender.Mask
	}
	return a
}
func (s *Server) mailAccounts(c *gin.Context) {
	var rows []model.MailAccount
	if e := s.db.WithContext(c.Request.Context()).Order("id DESC").Find(&rows).Error; e != nil {
		internal(c)
		return
	}
	for i := range rows {
		rows[i] = hideMail(rows[i])
	}
	ok(c, gin.H{"items": rows})
}
func (s *Server) getMailAccount(c *gin.Context) {
	a, found := record[model.MailAccount](s, c)
	if found {
		ok(c, hideMail(a))
	}
}
func (s *Server) createMailAccount(c *gin.Context) { s.saveMailAccount(c, nil) }
func (s *Server) putMailAccount(c *gin.Context) {
	a, found := record[model.MailAccount](s, c)
	if found {
		s.saveMailAccount(c, &a)
	}
}
func (s *Server) saveMailAccount(c *gin.Context, old *model.MailAccount) {
	var req model.MailAccount
	if !bind(c, &req) {
		return
	}
	if strings.ContainsAny(req.Host, "/\r\n ") || req.SMTPPort < 0 || req.SMTPPort > 65535 {
		fail(c, 400, "invalid_request", "邮箱主机或 SMTP 端口不正确")
		return
	}
	if req.SMTPTLS != "" && req.SMTPTLS != "ssl" && req.SMTPTLS != "starttls" && req.SMTPTLS != "none" {
		fail(c, 400, "invalid_request", "SMTP TLS 模式错误")
		return
	}
	cipher, e := crypto.New(s.config.Secret)
	if e != nil {
		internal(c)
		return
	}
	req.ID = 0
	req.CreatedAt = timeZero()
	req.UpdatedAt = timeZero()
	if old != nil {
		req.ID = old.ID
		req.CreatedAt = old.CreatedAt
	}
	if req.Password == sender.Mask || req.Password == "" && old != nil {
		if old == nil {
			fail(c, 400, "invalid_request", "新授权码不能使用打码值")
			return
		}
		req.Password = old.Password
	} else {
		if req.Password == "" {
			fail(c, 400, "invalid_request", "邮箱授权码不能为空")
			return
		}
		req.Password, e = cipher.Encrypt(req.Password)
		if e != nil {
			internal(c)
			return
		}
	}
	if e = s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Save(&req).Error; e != nil {
			return e
		}
		return audit(tx, c, "保存邮箱账号", strconv.Itoa(int(req.ID)))
	}); e != nil {
		internal(c)
		return
	}
	ok(c, hideMail(req))
}
func (s *Server) deleteMailAccount(c *gin.Context) {
	a, found := record[model.MailAccount](s, c)
	if !found {
		return
	}
	var count int64
	if e := s.db.WithContext(c.Request.Context()).Model(&model.UpstreamAccount{}).Where("mail_account_id = ?", a.ID).Count(&count).Error; e != nil {
		internal(c)
		return
	}
	if count > 0 {
		fail(c, 409, "conflict", "邮箱仍被上游账号引用")
		return
	}
	if e := s.db.WithContext(c.Request.Context()).Model(&model.MailRule{}).Where("mail_account_id = ?", a.ID).Count(&count).Error; e != nil {
		internal(c)
		return
	}
	if count > 0 {
		fail(c, 409, "conflict", "邮箱仍被邮件规则引用")
		return
	}
	s.deleteRecord(c, &a, "删除邮箱账号")
}
func (s *Server) testMailAccount(c *gin.Context) {
	a, found := record[model.MailAccount](s, c)
	if !found {
		return
	}
	if s.mail == nil {
		fail(c, 503, "unavailable", "邮件服务未启动")
		return
	}
	count, e := s.mail.Test(c.Request.Context(), a)
	if e != nil {
		fail(c, 400, "connection_failed", e.Error())
		return
	}
	ok(c, gin.H{"inbox_count": count})
}
func (s *Server) deleteRecord(c *gin.Context, row any, action string) {
	if e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Delete(row).Error; e != nil {
			return e
		}
		return audit(tx, c, action, c.Param("id"))
	}); e != nil {
		internal(c)
		return
	}
	ok(c, nil)
}
func (s *Server) upstreamAccounts(c *gin.Context) {
	pageResult[model.UpstreamAccount](c, s.db.WithContext(c.Request.Context()).Model(&model.UpstreamAccount{}))
}
func (s *Server) getUpstream(c *gin.Context) {
	a, found := record[model.UpstreamAccount](s, c)
	if found {
		ok(c, a)
	}
}
func (s *Server) createUpstream(c *gin.Context) { s.saveUpstream(c, nil) }
func (s *Server) putUpstream(c *gin.Context) {
	a, found := record[model.UpstreamAccount](s, c)
	if found {
		s.saveUpstream(c, &a)
	}
}
func (s *Server) saveUpstream(c *gin.Context, old *model.UpstreamAccount) {
	if old != nil {
		unlock, valid := s.lockUpstream(c, old.ID)
		if !valid {
			return
		}
		defer unlock()
	}
	var req model.UpstreamAccount
	if !bind(c, &req) {
		return
	}
	if req.Type == "mock" && !s.config.DevMode {
		fail(c, 400, "invalid_request", "模拟登录仅限开发模式")
		return
	}
	if e := mailwatch.Validate(req.Filter); e != nil {
		fail(c, 400, "invalid_request", e.Error())
		return
	}
	if req.Type == "dora" {
		for _, raw := range []string{req.IAMBase, req.BusinessBase, req.CheckURL} {
			u, e := url.Parse(raw)
			if e != nil || u.Host == "" || u.User != nil || u.Scheme != "http" && u.Scheme != "https" {
				fail(c, 400, "invalid_request", "Dora IAM、业务和保活 URL 必须明确配置")
				return
			}
		}
		var mail model.MailAccount
		if e := s.db.WithContext(c.Request.Context()).First(&mail, req.MailAccountID).Error; e != nil || !mail.Enabled {
			fail(c, 400, "invalid_request", "需关联启用的 IMAP 账号")
			return
		}
	}
	if req.AccountType == "" {
		req.AccountType = "email"
	}
	req.ID = 0
	req.CreatedAt = timeZero()
	req.UpdatedAt = timeZero()
	req.State = upstream.LoggedOut
	req.Failures = 0
	req.LastError = ""
	req.LastLoginAt = nil
	req.CookieJar = ""
	req.Challenge = ""
	req.ChallengeAt = nil
	if old != nil {
		req.ID = old.ID
		req.CreatedAt = old.CreatedAt
	}
	if old != nil {
		if e := s.cache.Del(c.Request.Context(), upstream.CookieKey(old.ID)); e != nil {
			internal(c)
			return
		}
	}
	if e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Save(&req).Error; e != nil {
			return e
		}
		return audit(tx, c, "保存上游账号", strconv.Itoa(int(req.ID)))
	}); e != nil {
		internal(c)
		return
	}
	if e := s.cache.Del(c.Request.Context(), upstream.CookieKey(req.ID)); e != nil {
		fail(c, 503, "unavailable", "账号已保存，Cookie 缓存失效失败")
		return
	}
	ok(c, req)
}
func (s *Server) deleteUpstream(c *gin.Context) {
	a, found := record[model.UpstreamAccount](s, c)
	if !found {
		return
	}
	unlock, valid := s.lockUpstream(c, a.ID)
	if !valid {
		return
	}
	defer unlock()
	var n int64
	if e := s.db.WithContext(c.Request.Context()).Model(&model.Collector{}).Where("upstream_account_id = ?", a.ID).Count(&n).Error; e != nil {
		internal(c)
		return
	}
	if n > 0 {
		fail(c, 409, "conflict", "上游账号仍被采集器引用")
		return
	}
	if e := s.cache.Del(c.Request.Context(), upstream.CookieKey(a.ID)); e != nil {
		internal(c)
		return
	}
	s.deleteRecord(c, &a, "删除上游账号")
}
func (s *Server) loginUpstream(c *gin.Context) {
	a, found := record[model.UpstreamAccount](s, c)
	if !found {
		return
	}
	if s.upstream == nil {
		fail(c, 503, "unavailable", "上游服务未启动")
		return
	}
	if e := audit(s.db.WithContext(c.Request.Context()), c, "上游登录", c.Param("id")); e != nil {
		internal(c)
		return
	}
	if e := s.upstream.Login(c.Request.Context(), a.ID, false); e != nil {
		fail(c, 400, "login_failed", "登录未完成，请查看账号状态或手填验证码")
		return
	}
	s.getUpstream(c)
}
func (s *Server) manualCode(c *gin.Context) {
	a, found := record[model.UpstreamAccount](s, c)
	if !found {
		return
	}
	var req struct {
		Code string `json:"code" binding:"required,max=128"`
	}
	if !bind(c, &req) {
		return
	}
	if s.upstream == nil {
		fail(c, 503, "unavailable", "上游服务未启动")
		return
	}
	if e := audit(s.db.WithContext(c.Request.Context()), c, "上游手填验证码", c.Param("id")); e != nil {
		internal(c)
		return
	}
	if e := s.upstream.ManualCode(c.Request.Context(), a.ID, req.Code); e != nil {
		fail(c, 400, "login_failed", "验证码处理失败或流程已过期")
		return
	}
	s.getUpstream(c)
}
func (s *Server) collectors(c *gin.Context) {
	var rows []model.Collector
	if e := s.db.WithContext(c.Request.Context()).Order("kind ASC").Find(&rows).Error; e != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"items": rows})
}
func (s *Server) putCollector(c *gin.Context) {
	var old model.Collector
	if e := s.db.WithContext(c.Request.Context()).First(&old, "kind = ?", c.Param("kind")).Error; e != nil {
		fail(c, 404, "not_found", "采集器不存在")
		return
	}
	var req model.Collector
	if !bind(c, &req) {
		return
	}
	req.Kind = old.Kind
	req.Name = old.Name
	if e := syncer.Validate(req, s.config.DevMode); e != nil {
		fail(c, 400, "invalid_request", e.Error())
		return
	}
	if req.UpstreamAccountID != 0 {
		var n int64
		if e := s.db.WithContext(c.Request.Context()).Model(&model.UpstreamAccount{}).Where("id = ?", req.UpstreamAccountID).Count(&n).Error; e != nil {
			internal(c)
			return
		}
		if n == 0 {
			fail(c, 400, "invalid_request", "上游账号不存在")
			return
		}
	}
	req.LastSuccessAt = old.LastSuccessAt
	req.LastError = old.LastError
	req.Failures = 0
	req.NextRunAt = nil
	if !req.Mock {
		req.LastError = collector.ErrUnimplemented.Error()
	}
	if e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Save(&req).Error; e != nil {
			return e
		}
		return audit(tx, c, "更新采集器", req.Kind)
	}); e != nil {
		internal(c)
		return
	}
	ok(c, req)
}
func (s *Server) syncCollector(c *gin.Context) {
	if s.syncer == nil {
		fail(c, 503, "unavailable", "同步服务未启动")
		return
	}
	if e := s.syncer.Trigger(c.Request.Context(), c.Param("kind"), "manual"); e != nil {
		fail(c, 400, "sync_failed", "采集器不存在、未启用或队列繁忙")
		return
	}
	if e := audit(s.db.WithContext(c.Request.Context()), c, "立即同步", c.Param("kind")); e != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"queued": true})
}
func (s *Server) mailRules(c *gin.Context) {
	pageResult[model.MailRule](c, s.db.WithContext(c.Request.Context()).Model(&model.MailRule{}))
}
func (s *Server) getMailRule(c *gin.Context) {
	r, found := record[model.MailRule](s, c)
	if found {
		ok(c, r)
	}
}
func (s *Server) createMailRule(c *gin.Context) { s.saveMailRule(c, nil) }
func (s *Server) putMailRule(c *gin.Context) {
	r, found := record[model.MailRule](s, c)
	if found {
		s.saveMailRule(c, &r)
	}
}
func (s *Server) saveMailRule(c *gin.Context, old *model.MailRule) {
	var req model.MailRule
	if !bind(c, &req) {
		return
	}
	if e := mailwatch.Validate(model.CodeFilter{From: req.From, Subject: req.Subject}); e != nil {
		fail(c, 400, "invalid_request", e.Error())
		return
	}
	if !collector.Known(req.Kind) {
		fail(c, 400, "invalid_request", "采集器类型未知")
		return
	}
	var n int64
	if e := s.db.WithContext(c.Request.Context()).Model(&model.MailAccount{}).Where("id = ?", req.MailAccountID).Count(&n).Error; e != nil {
		internal(c)
		return
	}
	if n == 0 {
		fail(c, 400, "invalid_request", "邮箱不存在")
		return
	}
	req.ID = 0
	req.CreatedAt = timeZero()
	req.UpdatedAt = timeZero()
	if old != nil {
		req.ID = old.ID
		req.CreatedAt = old.CreatedAt
	}
	if e := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Save(&req).Error; e != nil {
			return e
		}
		return audit(tx, c, "保存邮件规则", strconv.Itoa(int(req.ID)))
	}); e != nil {
		internal(c)
		return
	}
	ok(c, req)
}
func (s *Server) deleteMailRule(c *gin.Context) {
	r, found := record[model.MailRule](s, c)
	if found {
		s.deleteRecord(c, &r, "删除邮件规则")
	}
}
func (s *Server) syncLogs(c *gin.Context) {
	q := s.db.WithContext(c.Request.Context()).Model(&model.SyncLog{})
	if kind := c.Query("kind"); kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if status := c.Query("status"); status == "error" {
		q = q.Where("error <> ?", "")
	} else if status == "success" {
		q = q.Where("error = ? AND finished_at IS NOT NULL", "")
	}
	q, valid := timeRange(c, q, "started_at")
	if valid {
		pageResult[model.SyncLog](c, q)
	}
}
func (s *Server) lockUpstream(c *gin.Context, id uint) (func(), bool) {
	key := fmt.Sprintf("upstream-login:%d", id)
	token, e := s.cache.Lock(c.Request.Context(), key, time.Minute)
	if e != nil {
		if e == cache.ErrLock {
			fail(c, 409, "conflict", "账号正在登录或保活，请稍后修改")
		} else {
			internal(c)
		}
		return nil, false
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.cache.Unlock(ctx, key, token)
	}, true
}
