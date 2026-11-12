package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/db"
	"github.com/reputationly/ticket-hub/internal/mailwatch"
	"github.com/reputationly/ticket-hub/internal/model"
)

func TestDora(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/login/sms":
			var body map[string]string
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
			}
			if body["account"] != "test@example.com" || body["account_type"] != "email" || body["scene"] != "login" {
				t.Error(body)
			}
			if r.Header.Get("X-CSRFToken") != "csrf-before" {
				t.Error("发码缺少 CSRF")
			}
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "csrf-after", Path: "/"})
			_, _ = w.Write([]byte(`{"data":{"token":"challenge"}}`))
		case "/api/v1/login/account/code":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["code"] != "654321" || body["token"] != "challenge" || body["account_type"] != "configured-type" {
				t.Error(body)
			}
			if r.Header.Get("X-CSRFToken") != "csrf-after" {
				t.Error("验码缺少新 CSRF")
			}
			cookie, e := r.Cookie("csrftoken")
			if e != nil || cookie.Value != "csrf-after" {
				t.Error(cookie, e)
			}
			http.SetCookie(w, &http.Cookie{Name: "business_session", Value: "signed-in", Path: "/", MaxAge: 3600})
			_, _ = w.Write([]byte(`{}`))
		case "/keepalive":
			if cookie, e := r.Cookie("business_session"); e != nil || cookie.Value != "signed-in" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	a := model.UpstreamAccount{Type: "dora", Email: "test@example.com", IAMBase: server.URL, BusinessBase: server.URL, CheckURL: server.URL + "/keepalive", AccountType: "configured-type"}
	s, e := NewSession(a, "", server.Client())
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(server.URL)
	s.Jar.SetCookies(u, []*http.Cookie{{Name: "csrftoken", Value: "csrf-before", Path: "/"}})
	d := Dora{}
	token, e := d.SendCode(ctx, s)
	if e != nil || token != "challenge" {
		t.Fatal(token, e)
	}
	if e = d.VerifyCode(ctx, s, token, "654321"); e != nil {
		t.Fatal(e)
	}
	if e = d.Check(ctx, s); e != nil {
		t.Fatal(e)
	}
	data, e := s.Jar.Export()
	if e != nil {
		t.Fatal(e)
	}
	restored, e := NewSession(a, data, server.Client())
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Check(ctx, restored); e != nil {
		t.Fatal("Cookie 未保存", e)
	}
	empty, _ := NewSession(a, "", server.Client())
	if !errors.Is(d.Check(ctx, empty), ErrSession) {
		t.Fatal("空会话未拒绝")
	}
	empty.Account.CheckURL = ""
	if e = d.Check(ctx, empty); e == nil {
		t.Fatal("未配置保活应失败")
	}
}

type codeWaiter func(context.Context, uint, mailwatch.Filter) (string, error)

func (f codeWaiter) WaitCode(ctx context.Context, id uint, filter mailwatch.Filter) (string, error) {
	return f(ctx, id, filter)
}

type authFake struct{ fail bool }

func (a authFake) SendCode(context.Context, *Session) (string, error) {
	if a.fail {
		return "", errors.New("模拟错误")
	}
	return "challenge", nil
}
func (authFake) VerifyCode(ctx context.Context, s *Session, token, code string) error {
	if token != "challenge" || code != "654321" {
		return errors.New("验证码错误")
	}
	return nil
}
func (authFake) Check(context.Context, *Session) error { return nil }
func TestLoginService(t *testing.T) {
	d, e := db.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := d.DB()
	defer sql.Close()
	store := cache.NewMemory()
	defer store.Close()
	cipher, _ := crypto.New(strings.Repeat("s", 32))
	a := model.UpstreamAccount{Name: "测试", Type: "dora", Email: "test@example.com", Enabled: true, State: LoggedOut}
	if e = d.Create(&a).Error; e != nil {
		t.Fatal(e)
	}
	if e = d.Create(&model.User{Username: "admin", PasswordHash: "test", Role: "admin", Enabled: true}).Error; e != nil {
		t.Fatal(e)
	}
	s := &Service{DB: d, Cache: store, Cipher: cipher, Authenticators: map[string]Authenticator{"dora": authFake{}}, Waiter: codeWaiter(func(ctx context.Context, id uint, f mailwatch.Filter) (string, error) {
		var current model.UpstreamAccount
		d.First(&current, a.ID)
		if current.State != Waiting || f.Since.IsZero() {
			t.Error(current.State, f)
		}
		return "654321", nil
	})}
	ctx := context.Background()
	if e = s.Login(ctx, a.ID, false); e != nil {
		t.Fatal(e)
	}
	d.First(&a, a.ID)
	if a.State != Online || a.LastLoginAt == nil || a.CookieJar == "" || a.Challenge != "" {
		t.Fatal(a)
	}
	cached, e := store.Get(ctx, CookieKey(a.ID))
	if e != nil || cached != a.CookieJar || strings.Contains(cached, "Saved") {
		t.Fatal(cached, e)
	}
	key := "upstream-login:1"
	token, e := store.Lock(ctx, key, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if !errors.Is(s.Login(ctx, a.ID, false), cache.ErrLock) {
		t.Fatal("登录锁未生效")
	}
	_ = store.Unlock(ctx, key, token)
	s.Authenticators["dora"] = authFake{fail: true}
	for i := 0; i < 3; i++ {
		if e = s.Login(ctx, a.ID, true); e == nil {
			t.Fatal("失败未传播")
		}
	}
	d.First(&a, a.ID)
	if a.Failures != 3 || a.State != Failed {
		t.Fatal(a)
	}
	if e = s.Login(ctx, a.ID, true); e == nil {
		t.Fatal("三次失败仍自动登录")
	}
	var count int64
	d.Model(&model.Notification{}).Count(&count)
	if count != 1 {
		t.Fatal("管理员告警数量", count)
	}
	s.Authenticators["dora"] = authFake{}
	challenge, _ := cipher.Encrypt("challenge")
	d.Model(&a).Updates(map[string]any{"challenge": challenge, "challenge_at": time.Now(), "state": Waiting})
	if e = s.ManualCode(ctx, a.ID, "654321"); e != nil {
		t.Fatal(e)
	}
	d.First(&a, a.ID)
	if a.State != Online || a.Failures != 0 {
		t.Fatal(a)
	}
	if e = s.ManualCode(ctx, a.ID, "654321"); e == nil {
		t.Fatal("无流程手填未拒绝")
	}
}
func TestMockOnlyDevelopment(t *testing.T) {
	s := &Service{}
	if _, e := s.auth(model.UpstreamAccount{Type: "mock"}); e == nil {
		t.Fatal("生产允许 mock")
	}
	session, _ := NewSession(model.UpstreamAccount{}, "", nil)
	m := Mock{}
	if !errors.Is(m.Check(context.Background(), session), ErrSession) {
		t.Fatal("未登录 mock")
	}
	if e := m.VerifyCode(context.Background(), session, "mock-token", "wrong"); e == nil {
		t.Fatal("错误 mock 码")
	}
}
