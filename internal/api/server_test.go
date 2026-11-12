package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/auth"
	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/config"
	"github.com/reputationly/ticket-hub/internal/db"
	"github.com/reputationly/ticket-hub/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const testPassword = "测试管理员密码123"

type fixture struct {
	router http.Handler
	db     *gorm.DB
	store  *cache.Memory
	cfg    config.Config
}

func setup(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	d, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := d.DB()
	t.Cleanup(func() { _ = sql.Close() })
	if err = db.Bootstrap(d, "admin", testPassword); err != nil {
		t.Fatal(err)
	}
	store := cache.NewMemory()
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.Config{Secret: strings.Repeat("测试", 16)}
	return &fixture{New(d, store, cfg), d, store, cfg}
}
func request(t *testing.T, r http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func status(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("状态码=%d，期望=%d，响应=%s", w.Code, want, w.Body.String())
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("非 JSON 响应：%s", w.Body.String())
	}
}
func loginAs(t *testing.T, r http.Handler, name, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": name, "password": password})
	w := request(t, r, "POST", "/api/auth/login", string(body), nil)
	status(t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.CookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("会话 Cookie 属性错误")
	}
	if strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "token") {
		t.Fatal("响应泄露密码或令牌")
	}
	return cookies[0]
}
func create(t *testing.T, f *fixture, cookie *http.Cookie, name, role string) uint {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"username": name, "password": testPassword, "role": role, "display_name": "测试用户", "email": "user@example.com"})
	w := request(t, f.router, "POST", "/api/admin/users", string(body), cookie)
	status(t, w, 200)
	var result struct {
		Data model.User `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data.ID
}

func TestAuthLifecycle(t *testing.T) {
	f := setup(t)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", nil), 401)
	status(t, request(t, f.router, "POST", "/api/auth/login", "{}", nil), 400)
	status(t, request(t, f.router, "POST", "/api/auth/login", "{", nil), 400)
	status(t, request(t, f.router, "POST", "/api/auth/login", `{"username":"不存在","password":"错误密码"}`, nil), 401)
	cookie := loginAs(t, f.router, "admin", testPassword)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", cookie), 200)
	status(t, request(t, f.router, "POST", "/api/auth/logout", "", cookie), 200)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", cookie), 401)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", &http.Cookie{Name: auth.CookieName, Value: cookie.Value + "x"}), 401)
}
func TestLoginLimit(t *testing.T) {
	f := setup(t)
	for i := 0; i < 5; i++ {
		status(t, request(t, f.router, "POST", "/api/auth/login", `{"username":"admin","password":"错误密码"}`, nil), 401)
	}
	status(t, request(t, f.router, "POST", "/api/auth/login", `{"username":"admin","password":"测试管理员密码123"}`, nil), 429)
	var u model.User
	f.db.First(&u)
	token, _, err := auth.New(f.cfg.Secret).Issue(u, -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.router, "GET", "/api/auth/me", "", &http.Cookie{Name: auth.CookieName, Value: token}), 401)
}
func TestPassword(t *testing.T) {
	f := setup(t)
	cookie := loginAs(t, f.router, "admin", testPassword)
	status(t, request(t, f.router, "PUT", "/api/auth/password", `{"old_password":"错误","new_password":"新管理员密码123"}`, cookie), 400)
	status(t, request(t, f.router, "PUT", "/api/auth/password", `{"old_password":"测试管理员密码123","new_password":"短"}`, cookie), 400)
	status(t, request(t, f.router, "PUT", "/api/auth/password", `{"old_password":"测试管理员密码123","new_password":"新管理员密码123"}`, cookie), 200)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", cookie), 401)
	loginAs(t, f.router, "admin", "新管理员密码123")
}

func TestUsersLifecycle(t *testing.T) {
	f := setup(t)
	admin := loginAs(t, f.router, "admin", testPassword)
	status(t, request(t, f.router, "GET", "/api/admin/users/999", "", admin), 404)
	status(t, request(t, f.router, "GET", "/api/admin/users/no", "", admin), 400)
	status(t, request(t, f.router, "POST", "/api/admin/users", `{}`, admin), 400)
	status(t, request(t, f.router, "POST", "/api/admin/users", `{"username":"bad","password":"short","role":"user"}`, admin), 400)
	id := create(t, f, admin, "member", "user")
	createURL := fmt.Sprintf("/api/admin/users/%d", id)
	status(t, request(t, f.router, "POST", "/api/admin/users", `{"username":"member","password":"测试管理员密码123","role":"user"}`, admin), 409)
	status(t, request(t, f.router, "PUT", createURL, `{"username":"admin","role":"user"}`, admin), 409)
	wEmpty := request(t, f.router, "GET", "/api/admin/users?username=不存在", "", admin)
	status(t, wEmpty, 200)
	if !strings.Contains(wEmpty.Body.String(), `"items":[]`) || !strings.Contains(wEmpty.Body.String(), `"total":0`) {
		t.Fatal(wEmpty.Body.String())
	}
	member := loginAs(t, f.router, "member", testPassword)
	for _, path := range []string{"/api/admin/users", "/api/admin/settings", "/api/admin/audit-logs"} {
		status(t, request(t, f.router, "GET", path, "", member), 403)
	}
	status(t, request(t, f.router, "GET", createURL, "", admin), 200)
	w := request(t, f.router, "GET", "/api/admin/users?username=member&page=1&page_size=1", "", admin)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatal(w.Body.String())
	}
	status(t, request(t, f.router, "PUT", createURL, `{"username":"member","display_name":"修改后","role":"user","enabled":true}`, admin), 200)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", member), 401)
	member = loginAs(t, f.router, "member", testPassword)
	status(t, request(t, f.router, "POST", createURL+"/reset-password", `{"password":"重置后的密码123"}`, admin), 200)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", member), 401)
	member = loginAs(t, f.router, "member", "重置后的密码123")
	status(t, request(t, f.router, "PUT", createURL+"/disable", "", admin), 200)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", member), 401)
	status(t, request(t, f.router, "POST", "/api/auth/login", `{"username":"member","password":"重置后的密码123"}`, nil), 401)
	status(t, request(t, f.router, "PUT", createURL, `{"username":"member","role":"user","enabled":true}`, admin), 200)
	member = loginAs(t, f.router, "member", "重置后的密码123")
	status(t, request(t, f.router, "DELETE", createURL, "", admin), 200)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", member), 401)
	status(t, request(t, f.router, "GET", createURL, "", admin), 404)
	w = request(t, f.router, "GET", "/api/admin/audit-logs?page_size=100", "", admin)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), testPassword) || !strings.Contains(w.Body.String(), "创建用户") {
		t.Fatal("审计记录错误")
	}
}
func TestLastAdmin(t *testing.T) {
	f := setup(t)
	admin := loginAs(t, f.router, "admin", testPassword)
	for _, methodPath := range []struct{ method, path, body string }{{"DELETE", "/api/admin/users/1", ""}, {"PUT", "/api/admin/users/1/disable", ""}, {"PUT", "/api/admin/users/1", `{"username":"admin","role":"user"}`}} {
		status(t, request(t, f.router, methodPath.method, methodPath.path, methodPath.body, admin), 409)
	}
	status(t, request(t, f.router, "GET", "/api/auth/me", "", admin), 200)
}
func TestSettingsAndRoutes(t *testing.T) {
	f := setup(t)
	admin := loginAs(t, f.router, "admin", testPassword)
	w := request(t, f.router, "GET", "/api/admin/settings", "", admin)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"session_hours":24`) {
		t.Fatal(w.Body.String())
	}
	if _, err := f.store.Get(context.Background(), settingsKey); err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.router, "PUT", "/api/admin/settings", `{"site_name":"测试站点","public_url":"javascript:alert(1)","session_hours":2}`, admin), 400)
	status(t, request(t, f.router, "PUT", "/api/admin/settings", `{"site_name":"测试站点","session_hours":0}`, admin), 400)
	status(t, request(t, f.router, "PUT", "/api/admin/settings", `{"site_name":"测试站点","public_url":"https://example.com","session_hours":2}`, admin), 200)
	if _, err := f.store.Get(context.Background(), settingsKey); err != cache.ErrMiss {
		t.Fatalf("缓存未失效：%v", err)
	}
	w = request(t, f.router, "GET", "/api/admin/settings", "", admin)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"session_hours":2`) {
		t.Fatal(w.Body.String())
	}
	newCookie := loginAs(t, f.router, "admin", testPassword)
	claims, err := auth.New(f.cfg.Secret).Parse(newCookie.Value)
	if err != nil || time.Until(claims.ExpiresAt.Time) > 2*time.Hour {
		t.Fatal("会话时长设置未生效")
	}
	status(t, request(t, f.router, "GET", "/healthz", "", nil), 200)
	status(t, request(t, f.router, "GET", "/api/missing", "", nil), 404)
	status(t, request(t, f.router, "GET", "/some/page", "", nil), 404)
	req := httptest.NewRequest("PUT", "/api/admin/settings", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(admin)
	w = httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	status(t, w, 403)
	status(t, request(t, f.router, "POST", "/api/auth/login", "", nil), 400)
}
func TestCacheFailureClosed(t *testing.T) {
	f := setup(t)
	cookie := loginAs(t, f.router, "admin", testPassword)
	_ = f.store.Close()
	status(t, request(t, f.router, "GET", "/api/auth/me", "", cookie), 503)
	status(t, request(t, f.router, "POST", "/api/auth/login", `{"username":"admin","password":"测试管理员密码123"}`, nil), 503)
}

func TestPostgresHandlers(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_PG_DSN，跳过 PostgreSQL 集成测试")
	}
	gin.SetMode(gin.TestMode)
	d, err := db.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := d.DB()
	defer sql.Close()
	tx := d.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	hash, _ := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	name := fmt.Sprintf("pg_admin_%d", time.Now().UnixNano())
	u := model.User{Username: name, PasswordHash: string(hash), Role: "admin", Enabled: true}
	if err = tx.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	store := cache.NewMemory()
	defer store.Close()
	cfg := config.Config{Secret: strings.Repeat("pg-test-", 8)}
	f := &fixture{New(tx, store, cfg), tx, store, cfg}
	cookie := loginAs(t, f.router, name, testPassword)
	create(t, f, cookie, name+"_user", "user")
	status(t, request(t, f.router, "PUT", "/api/admin/settings", `{"site_name":"PG测试","session_hours":12}`, cookie), 200)
	status(t, request(t, f.router, "GET", "/api/admin/settings", "", cookie), 200)
	status(t, request(t, f.router, "GET", "/api/admin/audit-logs", "", cookie), 200)
	status(t, request(t, f.router, "POST", "/api/auth/logout", "", cookie), 200)
	status(t, request(t, f.router, "GET", "/api/auth/me", "", cookie), 401)
}
