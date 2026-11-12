package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type userInput struct {
	Username    string `json:"username" binding:"required,max=64"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name" binding:"max=100"`
	Role        string `json:"role" binding:"required,oneof=admin user"`
	Phone       string `json:"phone" binding:"max=32"`
	Email       string `json:"email" binding:"omitempty,email,max=254"`
	Enabled     *bool  `json:"enabled"`
}

func (s *Server) users(c *gin.Context) {
	page, size := pagination(c)
	var list []model.User
	var count int64
	q := s.db.WithContext(c.Request.Context()).Model(&model.User{})
	if name := c.Query("username"); name != "" {
		q = q.Where("username = ?", name)
	}
	if role := c.Query("role"); role != "" {
		q = q.Where("role = ?", role)
	}
	if enabled := c.Query("enabled"); enabled != "" {
		v, err := strconv.ParseBool(enabled)
		if err != nil {
			fail(c, 400, "invalid_request", "启用状态不正确")
			return
		}
		q = q.Where("enabled = ?", v)
	}
	if err := q.Count(&count).Error; err != nil {
		internal(c)
		return
	}
	if err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"items": list, "total": count, "page": page, "page_size": size})
}
func (s *Server) getUser(c *gin.Context) {
	if u, okFound := s.findUser(c); okFound {
		ok(c, u)
	}
}
func (s *Server) createUser(c *gin.Context) {
	var req userInput
	if !bind(c, &req) {
		return
	}
	if strings.TrimSpace(req.Username) != req.Username || !validPassword(req.Password) {
		fail(c, 400, "invalid_request", "用户名不可含首尾空格，密码需为 12~72 字节")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		internal(c)
		return
	}
	u := model.User{Username: req.Username, PasswordHash: string(hash), DisplayName: req.DisplayName, Role: req.Role, Phone: req.Phone, Email: req.Email, Enabled: true}
	if req.Enabled != nil {
		u.Enabled = *req.Enabled
	}
	err = s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&u).Error; e != nil {
			return e
		}
		return audit(tx, c, "创建用户", strconv.FormatUint(uint64(u.ID), 10))
	})
	if err != nil {
		var count int64
		if e := s.db.Model(&model.User{}).Where("username = ?", req.Username).Count(&count).Error; e == nil && count > 0 {
			fail(c, 409, "conflict", "用户名已存在")
		} else {
			internal(c)
		}
		return
	}
	ok(c, u)
}

var errLastAdmin = errors.New("必须保留至少一个启用的管理员")

func (s *Server) mutate(c *gin.Context, action string, fn func(*gorm.DB, *model.User) error) {
	ctx := c.Request.Context()
	token, err := s.cache.Lock(ctx, "admin-users", 30*time.Second)
	if err != nil {
		if errors.Is(err, cache.ErrLock) {
			fail(c, 409, "conflict", "用户正在变更，请稍后重试")
		} else {
			fail(c, 503, "unavailable", "缓存服务不可用")
		}
		return
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		_ = s.cache.Unlock(release, "admin-users", token)
	}()
	u, found := s.findUser(c)
	if !found {
		return
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := fn(tx, &u); e != nil {
			return e
		}
		var count int64
		if e := tx.Model(&model.User{}).Where("role = ? AND enabled = ?", "admin", true).Count(&count).Error; e != nil {
			return e
		}
		if count == 0 {
			return errLastAdmin
		}
		return audit(tx, c, action, strconv.FormatUint(uint64(u.ID), 10))
	})
	if errors.Is(err, errLastAdmin) {
		fail(c, 409, "last_admin", err.Error())
		return
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		fail(c, 409, "conflict", "用户名已存在")
		return
	}
	if err != nil {
		internal(c)
		return
	}
	ok(c, u)
}
func (s *Server) updateUser(c *gin.Context) {
	var req userInput
	if !bind(c, &req) {
		return
	}
	if req.Password != "" || strings.TrimSpace(req.Username) != req.Username {
		fail(c, 400, "invalid_request", "请使用重置密码接口，用户名不可含首尾空格")
		return
	}
	s.mutate(c, "更新用户", func(tx *gorm.DB, u *model.User) error {
		var count int64
		if err := tx.Model(&model.User{}).Where("username = ? AND id <> ?", req.Username, u.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return gorm.ErrDuplicatedKey
		}
		values := map[string]any{"username": req.Username, "display_name": req.DisplayName, "role": req.Role, "phone": req.Phone, "email": req.Email, "auth_version": gorm.Expr("auth_version + ?", 1)}
		if req.Enabled != nil {
			values["enabled"] = *req.Enabled
		}
		if err := tx.Model(&model.User{}).Where("id = ?", u.ID).Updates(values).Error; err != nil {
			return err
		}
		return tx.First(u, u.ID).Error
	})
}
func (s *Server) deleteUser(c *gin.Context) {
	s.mutate(c, "删除用户", func(tx *gorm.DB, u *model.User) error { return tx.Delete(u).Error })
}
func (s *Server) disableUser(c *gin.Context) {
	s.mutate(c, "禁用用户", func(tx *gorm.DB, u *model.User) error {
		if err := tx.Model(&model.User{}).Where("id = ?", u.ID).Updates(map[string]any{"enabled": false, "auth_version": gorm.Expr("auth_version + ?", 1)}).Error; err != nil {
			return err
		}
		return tx.First(u, u.ID).Error
	})
}
func (s *Server) resetPassword(c *gin.Context) {
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if !bind(c, &req) {
		return
	}
	if !validPassword(req.Password) {
		fail(c, 400, "invalid_request", "密码需为 12~72 字节")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		internal(c)
		return
	}
	s.mutate(c, "重置用户密码", func(tx *gorm.DB, u *model.User) error {
		if err := tx.Model(&model.User{}).Where("id = ?", u.ID).Updates(map[string]any{"password_hash": string(hash), "auth_version": gorm.Expr("auth_version + ?", 1)}).Error; err != nil {
			return err
		}
		return tx.First(u, u.ID).Error
	})
}
func (s *Server) auditLogs(c *gin.Context) {
	page, size := pagination(c)
	var rows []model.AuditLog
	var count int64
	q := s.db.WithContext(c.Request.Context()).Model(&model.AuditLog{})
	if err := q.Count(&count).Error; err != nil {
		internal(c)
		return
	}
	if err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"items": rows, "total": count, "page": page, "page_size": size})
}
