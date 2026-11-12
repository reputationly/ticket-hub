package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/reputationly/ticket-hub/internal/model"
)

var ErrSession = errors.New("上游会话已失效")

type Authenticator interface {
	SendCode(context.Context, *Session) (string, error)
	VerifyCode(context.Context, *Session, string, string) error
	Check(context.Context, *Session) error
}
type Session struct {
	Account model.UpstreamAccount
	Client  *http.Client
	Jar     *Jar
	Filters map[string]any
}
type Jar struct {
	base  *cookiejar.Jar
	mu    sync.Mutex
	Saved map[string][]*http.Cookie
}

func NewJar(encoded string) (*Jar, error) {
	base, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	j := &Jar{base: base, Saved: map[string][]*http.Cookie{}}
	if encoded != "" {
		if e := json.Unmarshal([]byte(encoded), &j.Saved); e != nil {
			return nil, errors.New("Cookie 数据损坏")
		}
		for raw, cookies := range j.Saved {
			u, e := url.Parse(raw)
			if e != nil {
				return nil, e
			}
			base.SetCookies(u, cookies)
		}
	}
	return j, nil
}
func (j *Jar) Cookies(u *url.URL) []*http.Cookie { return j.base.Cookies(u) }
func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.base.SetCookies(u, cookies)
	j.mu.Lock()
	defer j.mu.Unlock()
	origin := u.Scheme + "://" + u.Host
	all := j.Saved[origin]
	for _, c := range cookies {
		copy := *c
		if copy.Path == "" || !strings.HasPrefix(copy.Path, "/") {
			copy.Path = "/"
			if i := strings.LastIndex(u.Path, "/"); i > 0 {
				copy.Path = u.Path[:i]
			}
		}
		if copy.MaxAge > 0 {
			copy.Expires = time.Now().Add(time.Duration(copy.MaxAge) * time.Second)
			copy.MaxAge = 0
		}
		found := false
		for i, old := range all {
			if old.Name == copy.Name && old.Path == copy.Path && old.Domain == copy.Domain {
				all[i] = &copy
				found = true
				break
			}
		}
		if !found {
			all = append(all, &copy)
		}
	}
	j.Saved[origin] = all
}
func (j *Jar) Export() (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	data, e := json.Marshal(j.Saved)
	return string(data), e
}
func NewSession(a model.UpstreamAccount, encoded string, client *http.Client) (*Session, error) {
	j, e := NewJar(encoded)
	if e != nil {
		return nil, e
	}
	c := http.Client{Timeout: 20 * time.Second}
	if client != nil {
		c = *client
	}
	c.Jar = j
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 8 {
			return errors.New("跳转过多")
		}
		return nil
	}
	return &Session{Account: a, Client: &c, Jar: j}, nil
}

type Dora struct{}

func (d Dora) post(ctx context.Context, s *Session, path string, body any) (map[string]json.RawMessage, error) {
	data, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	endpoint := strings.TrimRight(s.Account.IAMBase, "/") + path
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if e != nil {
		return nil, errors.New("IAM 地址错误")
	}
	req.Header.Set("Content-Type", "application/json")
	for _, c := range s.Jar.Cookies(req.URL) {
		if c.Name == "csrftoken" {
			req.Header.Set("X-CSRFToken", c.Value)
		}
	}
	resp, e := s.Client.Do(req)
	if e != nil {
		return nil, errors.New("IAM 请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("IAM 返回非成功状态")
	}
	var result map[string]json.RawMessage
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); e != nil {
		return nil, errors.New("IAM 响应不是 JSON")
	}
	return result, nil
}
func (d Dora) SendCode(ctx context.Context, s *Session) (string, error) {
	// 待抓包确认：响应结构及登录前 CSRF 初始化步骤，以实际 IAM 会话为准。
	result, e := d.post(ctx, s, "/api/v1/login/sms", map[string]string{"account": s.Account.Email, "account_type": "email", "scene": "login"})
	if e != nil {
		return "", e
	}
	var token string
	_ = json.Unmarshal(result["token"], &token)
	if token == "" {
		var nested struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(result["data"], &nested)
		token = nested.Token
	}
	if token == "" {
		return "", errors.New("IAM 响应缺少验证码 token，待抓包确认")
	}
	return token, nil
}
func (d Dora) VerifyCode(ctx context.Context, s *Session, token, code string) error {
	// 待抓包确认：account_type、OIDC 业务回调与实际成功响应；不推测业务接口。
	typ := s.Account.AccountType
	if typ == "" {
		typ = "email"
	}
	_, e := d.post(ctx, s, "/api/v1/login/account/code", map[string]string{"code": code, "token": token, "account_type": typ})
	return e
}
func (Dora) Check(ctx context.Context, s *Session) error {
	// 待抓包确认：管理员必须配置已确认的只读保活 URL，不能把业务首页 200 当作登录成功。
	if s.Account.CheckURL == "" {
		return errors.New("保活 URL 未配置，待抓包确认")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", s.Account.CheckURL, nil)
	if e != nil {
		return e
	}
	client := *s.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := client.Do(req)
	if e != nil {
		return errors.New("保活请求失败")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return ErrSession
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("保活返回非成功状态")
	}
	return nil
}

type Mock struct{}

func (Mock) SendCode(context.Context, *Session) (string, error) { return "mock-token", nil }
func (Mock) VerifyCode(ctx context.Context, s *Session, token, code string) error {
	if token != "mock-token" || code != "123456" {
		return errors.New("模拟验证码错误")
	}
	u, _ := url.Parse("https://mock.invalid")
	s.Jar.SetCookies(u, []*http.Cookie{{Name: "mock_session", Value: "online", Path: "/", Expires: time.Now().Add(time.Hour)}})
	return nil
}
func (Mock) Check(ctx context.Context, s *Session) error {
	u, _ := url.Parse("https://mock.invalid")
	for _, c := range s.Jar.Cookies(u) {
		if c.Name == "mock_session" && c.Value == "online" {
			return nil
		}
	}
	return ErrSession
}
