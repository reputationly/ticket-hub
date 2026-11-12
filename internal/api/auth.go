package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/auth"
	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (s *Server) cookie(c *gin.Context, token string, age int) {
	c.SetSameSite(2)
	c.SetCookie(auth.CookieName, token, age, "/", "", s.config.CookieSecure, true)
}
func (s *Server) requireAuth(c *gin.Context) {
	raw, err := c.Cookie(auth.CookieName)
	if err != nil {
		fail(c, 401, "unauthorized", "请先登录")
		return
	}
	claims, err := s.auth.Parse(raw)
	if err != nil {
		fail(c, 401, "unauthorized", "会话已失效")
		return
	}
	_, err = s.cache.Get(c.Request.Context(), "blacklist:"+claims.ID)
	if err == nil {
		fail(c, 401, "unauthorized", "会话已退出")
		return
	}
	if !errors.Is(err, cache.ErrMiss) {
		fail(c, 503, "unavailable", "会话服务不可用")
		return
	}
	id, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || id == 0 {
		fail(c, 401, "unauthorized", "会话无效")
		return
	}
	var user model.User
	err = s.db.WithContext(c.Request.Context()).First(&user, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (!user.Enabled || user.AuthVersion != claims.Version) {
		fail(c, 401, "unauthorized", "用户或会话已失效")
		return
	}
	if err != nil {
		internal(c)
		return
	}
	c.Set("user", user)
	c.Set("claims", claims)
	c.Next()
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required,max=64"`
		Password string `json:"password" binding:"required,max=72"`
	}
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	sum := sha256.Sum256([]byte(req.Username))
	key := "login-fail:" + hex.EncodeToString(sum[:])
	lock, err := s.cache.Lock(ctx, key, 15*time.Second)
	if err != nil {
		if errors.Is(err, cache.ErrLock) {
			fail(c, 429, "rate_limited", "请稍后重试")
		} else {
			fail(c, 503, "unavailable", "登录服务不可用")
		}
		return
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.cache.Unlock(release, key, lock)
	}()
	failures, err := s.cache.Get(ctx, key)
	if err != nil && !errors.Is(err, cache.ErrMiss) {
		fail(c, 503, "unavailable", "登录服务不可用")
		return
	}
	if n, _ := strconv.Atoi(failures); n >= 5 {
		fail(c, 429, "rate_limited", "登录失败次数过多，请在 15 分钟后重试")
		return
	}
	var user model.User
	err = s.db.WithContext(ctx).Where("username = ?", req.Username).First(&user).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		internal(c)
		return
	}
	hash := user.PasswordHash
	if err != nil {
		hash = dummyHash
	}
	invalid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil
	if err != nil || invalid || !user.Enabled {
		if _, e := s.cache.Incr(ctx, key, 15*time.Minute); e != nil {
			fail(c, 503, "unavailable", "登录服务不可用")
			return
		}
		fail(c, 401, "invalid_credentials", "用户名或密码错误，或账号已停用")
		return
	}
	settings, err := s.settings(ctx)
	if err != nil {
		internal(c)
		return
	}
	ttl := time.Duration(settings.SessionHours) * time.Hour
	token, _, err := s.auth.Issue(user, ttl)
	if err != nil {
		internal(c)
		return
	}
	if err = s.cache.Del(ctx, key); err != nil {
		fail(c, 503, "unavailable", "登录服务不可用")
		return
	}
	s.cookie(c, token, int(ttl.Seconds()))
	ok(c, user)
}

var dummyHash = func() string {
	v, _ := bcrypt.GenerateFromPassword([]byte("不存在的账号占位密码"), bcrypt.DefaultCost)
	return string(v)
}()

func (s *Server) logout(c *gin.Context) {
	claims := currentClaims(c)
	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl > 0 {
		if err := s.cache.Set(c.Request.Context(), "blacklist:"+claims.ID, "1", ttl); err != nil {
			fail(c, 503, "unavailable", "退出失败，请重试")
			return
		}
	}
	s.cookie(c, "", -1)
	ok(c, nil)
}
func validPassword(p string) bool { return len(p) >= 12 && len(p) <= 72 }
func (s *Server) password(c *gin.Context) {
	var req struct {
		Old string `json:"old_password" binding:"required,max=72"`
		New string `json:"new_password" binding:"required"`
	}
	if !bind(c, &req) {
		return
	}
	if !validPassword(req.New) {
		fail(c, 400, "invalid_request", "新密码需为 12~72 字节")
		return
	}
	u := currentUser(c)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Old)) != nil {
		fail(c, 400, "invalid_password", "原密码不正确")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err != nil {
		internal(c)
		return
	}
	err = s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.User{}).Where("id = ? AND auth_version = ?", u.ID, u.AuthVersion).Updates(map[string]any{"password_hash": string(hash), "auth_version": gorm.Expr("auth_version + ?", 1)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("会话已变更")
		}
		return audit(tx, c, "修改密码", strconv.FormatUint(uint64(u.ID), 10))
	})
	if err != nil {
		internal(c)
		return
	}
	s.cookie(c, "", -1)
	ok(c, nil)
}
