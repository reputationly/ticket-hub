package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/auth"
	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/config"
	"github.com/reputationly/ticket-hub/internal/mailwatch"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/notifier"
	"github.com/reputationly/ticket-hub/internal/syncer"
	"github.com/reputationly/ticket-hub/internal/ticket"
	"github.com/reputationly/ticket-hub/internal/upstream"
	"github.com/reputationly/ticket-hub/web"
	"gorm.io/gorm"
)

type Server struct {
	db       *gorm.DB
	cache    cache.Cache
	auth     *auth.Service
	config   config.Config
	notify   *notifier.Service
	tickets  *ticket.Service
	mail     *mailwatch.Hub
	upstream *upstream.Service
	syncer   *syncer.Scheduler
}
type response struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func ok(c *gin.Context, data any) { c.JSON(http.StatusOK, response{"ok", "成功", data}) }
func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, response{code, message, nil})
}
func internal(c *gin.Context) { fail(c, 500, "internal_error", "服务暂不可用") }

func New(d *gorm.DB, store cache.Cache, cfg config.Config, services ...*notifier.Service) *gin.Engine {
	var notify *notifier.Service
	if len(services) > 0 {
		notify = services[0]
	}
	return NewWithUpstream(d, store, cfg, notify, nil, nil, nil)
}
func NewWithUpstream(d *gorm.DB, store cache.Cache, cfg config.Config, notify *notifier.Service, mail *mailwatch.Hub, accounts *upstream.Service, scheduler *syncer.Scheduler) *gin.Engine {
	s := &Server{db: d, cache: store, auth: auth.New(cfg.Secret), config: cfg}
	s.notify = notify
	s.mail = mail
	s.upstream = accounts
	s.syncer = scheduler
	s.tickets = &ticket.Service{DB: d, Cache: store, Dispatcher: s.notify}
	if s.notify == nil {
		s.tickets.Dispatcher = nil
	} else {
		s.notify.PublicURL = func(ctx context.Context) (string, error) {
			settings, e := s.settings(ctx)
			return settings.PublicURL, e
		}
	}
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("HTTP 请求", "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "duration", time.Since(start))
	})
	r.Use(func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				slog.Error("请求处理异常")
				internal(c)
			}
		}()
		c.Next()
	})
	r.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Header("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.Header("Cache-Control", "no-store")
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Request.Method != "OPTIONS" {
			if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
				fail(c, 403, "forbidden", "不允许跨站请求")
				return
			}
			if origin := c.GetHeader("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != c.Request.Host || (u.Scheme != "http" && u.Scheme != "https") {
					fail(c, 403, "forbidden", "请求来源不匹配")
					return
				}
			}
		}
		c.Next()
	})
	r.GET("/healthz", func(c *gin.Context) {
		sql, err := d.DB()
		if err != nil || sql.PingContext(c.Request.Context()) != nil {
			fail(c, 503, "unavailable", "数据库不可用")
			return
		}
		if err = store.Set(c.Request.Context(), "health", "ok", time.Second*10); err != nil {
			fail(c, 503, "unavailable", "缓存不可用")
			return
		}
		ok(c, gin.H{"status": "ok"})
	})
	r.POST("/api/auth/login", s.login)
	secured := r.Group("/api", s.requireAuth)
	secured.POST("/auth/logout", s.logout)
	secured.GET("/auth/me", func(c *gin.Context) { ok(c, currentUser(c)) })
	secured.PUT("/auth/password", s.password)
	admin := secured.Group("/admin", func(c *gin.Context) {
		if currentUser(c).Role != "admin" {
			fail(c, 403, "forbidden", "仅管理员可操作")
			return
		}
		c.Next()
	})
	admin.GET("/users", s.users)
	admin.POST("/users", s.createUser)
	admin.GET("/users/:id", s.getUser)
	admin.PUT("/users/:id", s.updateUser)
	admin.DELETE("/users/:id", s.deleteUser)
	admin.POST("/users/:id/reset-password", s.resetPassword)
	admin.PUT("/users/:id/disable", s.disableUser)
	admin.GET("/settings", s.getSettings)
	admin.PUT("/settings", s.putSettings)
	admin.GET("/audit-logs", s.auditLogs)
	s.notificationRoutes(secured, admin)
	s.upstreamRoutes(admin)
	r.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == "/api" || strings.HasPrefix(c.Request.URL.Path, "/api/") {
			fail(c, 404, "not_found", "接口不存在")
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			fail(c, 404, "not_found", "页面不存在")
			return
		}
		name := path.Clean("dist/" + strings.TrimPrefix(c.Request.URL.Path, "/"))
		if !strings.HasPrefix(name, "dist/") {
			name = "dist/index.html"
		}
		if data, err := fs.ReadFile(web.Files, name); err == nil {
			c.Data(200, contentType(name), data)
			return
		}
		data, err := fs.ReadFile(web.Files, "dist/index.html")
		if err != nil {
			fail(c, 404, "frontend_unavailable", "前端尚未构建")
			return
		}
		c.Data(200, "text/html; charset=utf-8", data)
	})
	return r
}
func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}
func currentUser(c *gin.Context) model.User     { return c.MustGet("user").(model.User) }
func currentClaims(c *gin.Context) *auth.Claims { return c.MustGet("claims").(*auth.Claims) }
func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		fail(c, 400, "invalid_request", "请求参数不正确")
		return false
	}
	return true
}
func pagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}
func (s *Server) findUser(c *gin.Context) (model.User, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, 400, "invalid_request", "用户编号不正确")
		return model.User{}, false
	}
	var u model.User
	err = s.db.WithContext(c.Request.Context()).First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fail(c, 404, "not_found", "用户不存在")
		return u, false
	}
	if err != nil {
		internal(c)
		return u, false
	}
	return u, true
}
func audit(tx *gorm.DB, c *gin.Context, action, target string) error {
	return tx.Create(&model.AuditLog{UserID: currentUser(c).ID, Action: action, Target: target, IP: c.ClientIP()}).Error
}
