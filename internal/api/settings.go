package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Settings struct {
	SiteName     string `json:"site_name" binding:"required,max=100"`
	LogoURL      string `json:"logo_url" binding:"max=2048"`
	PublicURL    string `json:"public_url" binding:"max=2048"`
	SessionHours int    `json:"session_hours" binding:"min=1,max=720"`
}

const settingsKey = "settings:site:v1"

func (s *Server) settingsLock(ctx context.Context) (func(), error) {
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		token, err := s.cache.Lock(deadline, "settings", 30*time.Second)
		if err == nil {
			return func() {
				release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = s.cache.Unlock(release, "settings", token)
			}, nil
		}
		if !errors.Is(err, cache.ErrLock) {
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-deadline.Done():
			timer.Stop()
			return nil, deadline.Err()
		case <-timer.C:
		}
	}
}

func (s *Server) settings(ctx context.Context) (Settings, error) {
	release, err := s.settingsLock(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer release()
	v, err := s.cache.Get(ctx, settingsKey)
	if err == nil {
		var cfg Settings
		if err = json.Unmarshal([]byte(v), &cfg); err != nil {
			return cfg, err
		}
		return cfg, nil
	}
	if !errors.Is(err, cache.ErrMiss) {
		return Settings{}, err
	}
	cfg := Settings{SiteName: "工单中心", SessionHours: 24}
	var rows []model.Setting
	if err = s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return cfg, err
	}
	for _, row := range rows {
		var target any
		switch row.Key {
		case "site_name":
			target = &cfg.SiteName
		case "logo_url":
			target = &cfg.LogoURL
		case "public_url":
			target = &cfg.PublicURL
		case "session_hours":
			target = &cfg.SessionHours
		default:
			continue
		}
		if err = json.Unmarshal([]byte(row.Value), target); err != nil {
			return cfg, err
		}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	err = s.cache.Set(ctx, settingsKey, string(data), 5*time.Minute)
	return cfg, err
}
func (s *Server) getSettings(c *gin.Context) {
	cfg, err := s.settings(c.Request.Context())
	if err != nil {
		internal(c)
		return
	}
	ok(c, cfg)
}
func validURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
func (s *Server) putSettings(c *gin.Context) {
	var cfg Settings
	if !bind(c, &cfg) {
		return
	}
	if !validURL(cfg.LogoURL) || !validURL(cfg.PublicURL) {
		fail(c, 400, "invalid_request", "网址必须为 HTTP 或 HTTPS 地址")
		return
	}
	release, err := s.settingsLock(c.Request.Context())
	if err != nil {
		fail(c, 503, "unavailable", "配置服务不可用")
		return
	}
	defer release()
	err = s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		values := map[string]any{"site_name": cfg.SiteName, "logo_url": cfg.LogoURL, "public_url": cfg.PublicURL, "session_hours": cfg.SessionHours}
		for key, value := range values {
			data, e := json.Marshal(value)
			if e != nil {
				return e
			}
			if e = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"})}).Create(&model.Setting{Key: key, Value: string(data)}).Error; e != nil {
				return e
			}
		}
		return audit(tx, c, "更新站点设置", "site")
	})
	if err != nil {
		internal(c)
		return
	}
	if err = s.cache.Del(c.Request.Context(), settingsKey); err != nil {
		fail(c, 503, "cache_invalidation_failed", "设置已保存，但缓存失效失败，请重试")
		return
	}
	ok(c, cfg)
}
