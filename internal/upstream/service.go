package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/mailwatch"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/sender"
	"gorm.io/gorm"
)

const (
	LoggedOut = "未登录"
	Sending   = "发码中"
	Waiting   = "等待验证码"
	LoggingIn = "登录中"
	Online    = "在线"
	Expired   = "过期"
	Failed    = "失败"
)

type CodeWaiter interface {
	WaitCode(context.Context, uint, mailwatch.Filter) (string, error)
}
type Service struct {
	DB             *gorm.DB
	Cache          cache.Cache
	Cipher         *crypto.Cipher
	Waiter         CodeWaiter
	HTTPClient     *http.Client
	DevMode        bool
	Authenticators map[string]Authenticator
}

func (s *Service) auth(a model.UpstreamAccount) (Authenticator, error) {
	if a.Type == "mock" && !s.DevMode {
		return nil, errors.New("模拟登录仅限开发模式")
	}
	if impl := s.Authenticators[a.Type]; impl != nil {
		return impl, nil
	}
	switch a.Type {
	case "dora":
		return Dora{}, nil
	case "mock":
		return Mock{}, nil
	}
	return nil, errors.New("未知登录类型")
}
func CookieKey(id uint) string { return fmt.Sprintf("upstream:cookies:%d", id) }
func (s *Service) Load(ctx context.Context, id uint) (*Session, Authenticator, error) {
	var a model.UpstreamAccount
	if e := s.DB.WithContext(ctx).First(&a, id).Error; e != nil {
		return nil, nil, e
	}
	if !a.Enabled {
		return nil, nil, errors.New("上游账号未启用")
	}
	impl, e := s.auth(a)
	if e != nil {
		return nil, nil, e
	}
	encrypted, e := s.Cache.Get(ctx, CookieKey(id))
	if errors.Is(e, cache.ErrMiss) {
		encrypted = a.CookieJar
	} else if e != nil {
		return nil, nil, e
	}
	plain := ""
	if encrypted != "" {
		plain, e = s.Cipher.Decrypt(encrypted)
		if e != nil {
			return nil, nil, errors.New("上游 Cookie 解密失败")
		}
	}
	session, e := NewSession(a, plain, s.HTTPClient)
	return session, impl, e
}
func (s *Service) save(ctx context.Context, session *Session, values map[string]any) error {
	data, e := session.Jar.Export()
	if e != nil {
		return e
	}
	encrypted, e := s.Cipher.Encrypt(data)
	if e != nil {
		return e
	}
	values["cookie_jar"] = encrypted
	if e = s.DB.WithContext(ctx).Model(&model.UpstreamAccount{}).Where("id = ?", session.Account.ID).Updates(values).Error; e != nil {
		return e
	}
	return s.Cache.Set(ctx, CookieKey(session.Account.ID), encrypted, time.Hour)
}
func (s *Service) state(ctx context.Context, id uint, state string) error {
	return s.DB.WithContext(ctx).Model(&model.UpstreamAccount{}).Where("id = ?", id).Update("state", state).Error
}
func (s *Service) Login(ctx context.Context, id uint, automatic bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	token, e := s.Cache.Lock(ctx, fmt.Sprintf("upstream-login:%d", id), 12*time.Minute)
	if e != nil {
		return e
	}
	defer func() { _ = s.Cache.Unlock(context.Background(), fmt.Sprintf("upstream-login:%d", id), token) }()
	session, impl, e := s.Load(ctx, id)
	if e != nil {
		return e
	}
	if automatic && session.Account.Failures >= 3 {
		return errors.New("连续失败三次，自动登录已停止")
	}
	if e = s.Cache.Del(ctx, fmt.Sprintf("upstream:manual:%d", id)); e != nil {
		return e
	}
	var code, challenge string
	since := time.Now().Truncate(time.Second)
	fail := func(cause error) error {
		safe, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failure(safe, id)
		return cause
	}
	if e = s.state(ctx, id, Sending); e != nil {
		return e
	}
	challenge, e = impl.SendCode(ctx, session)
	if e != nil {
		return fail(e)
	}
	encrypted, e := s.Cipher.Encrypt(challenge)
	if e != nil {
		return fail(e)
	}
	if e = s.save(ctx, session, map[string]any{"challenge": encrypted, "challenge_at": since, "state": Waiting, "last_error": ""}); e != nil {
		return fail(e)
	}
	if session.Account.Type == "mock" {
		code = "123456"
	} else {
		if s.Waiter == nil {
			return fail(errors.New("邮件监听器未启动"))
		}
		child, stop := context.WithCancel(ctx)
		defer stop()
		type result struct {
			code string
			err  error
		}
		codes := make(chan result, 1)
		go func() {
			v, e := s.Waiter.WaitCode(child, session.Account.MailAccountID, mailwatch.Filter{From: session.Account.Filter.From, Subject: session.Account.Filter.Subject, Code: session.Account.Filter.Code, Since: since})
			codes <- result{v, e}
		}()
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
	waiting:
		for {
			select {
			case <-ctx.Done():
				return fail(ctx.Err())
			case result := <-codes:
				if result.err != nil {
					return fail(result.err)
				}
				code = result.code
				break waiting
			case <-ticker.C:
				raw, e := s.Cache.Get(ctx, fmt.Sprintf("upstream:manual:%d", id))
				if e == nil {
					code, e = s.Cipher.Decrypt(raw)
					if e != nil {
						return fail(e)
					}
					_ = s.Cache.Del(ctx, fmt.Sprintf("upstream:manual:%d", id))
					break waiting
				}
				if !errors.Is(e, cache.ErrMiss) {
					return fail(e)
				}
			}
		}
	}
	if e = s.verify(ctx, session, impl, challenge, code); e != nil {
		return fail(e)
	}
	return nil
}
func (s *Service) verify(ctx context.Context, session *Session, impl Authenticator, token, code string) error {
	if e := s.state(ctx, session.Account.ID, LoggingIn); e != nil {
		return e
	}
	if e := impl.VerifyCode(ctx, session, token, code); e != nil {
		return e
	}
	if e := impl.Check(ctx, session); e != nil {
		return e
	}
	return s.save(ctx, session, map[string]any{"state": Online, "last_login_at": time.Now(), "failures": 0, "last_error": "", "challenge": "", "challenge_at": nil})
}
func (s *Service) ManualCode(ctx context.Context, id uint, code string) error {
	session, impl, e := s.Load(ctx, id)
	if e != nil {
		return e
	}
	if session.Account.ChallengeAt == nil || time.Since(*session.Account.ChallengeAt) > 10*time.Minute {
		return errors.New("验证码流程不存在或已过期，请重新发起登录")
	}
	encrypted, e := s.Cipher.Encrypt(code)
	if e != nil {
		return e
	}
	key := fmt.Sprintf("upstream-login:%d", id)
	lock, e := s.Cache.Lock(ctx, key, 12*time.Minute)
	if errors.Is(e, cache.ErrLock) {
		return s.Cache.Set(ctx, fmt.Sprintf("upstream:manual:%d", id), encrypted, time.Minute)
	}
	if e != nil {
		return e
	}
	defer s.Cache.Unlock(context.Background(), key, lock)
	session, impl, e = s.Load(ctx, id)
	if e != nil {
		return e
	}
	if session.Account.ChallengeAt == nil || time.Since(*session.Account.ChallengeAt) > 10*time.Minute {
		return errors.New("验证码流程已过期")
	}
	token, e := s.Cipher.Decrypt(session.Account.Challenge)
	if e != nil {
		return e
	}
	if e = s.verify(ctx, session, impl, token, code); e != nil {
		_ = s.failure(ctx, id)
	}
	return e
}
func (s *Service) failure(ctx context.Context, id uint) error {
	var failures int
	e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Model(&model.UpstreamAccount{}).Where("id = ?", id).Updates(map[string]any{"state": Failed, "failures": gorm.Expr("failures + 1"), "last_error": "登录失败，请检查配置、验证码与连接"}).Error; e != nil {
			return e
		}
		var a model.UpstreamAccount
		if e := tx.First(&a, id).Error; e != nil {
			return e
		}
		failures = a.Failures
		return nil
	})
	if e != nil {
		return e
	}
	if failures == 3 {
		var admins []model.User
		if e = s.DB.WithContext(ctx).Where("role = ? AND enabled = ?", "admin", true).Find(&admins).Error; e != nil {
			return e
		}
		for _, u := range admins {
			n := sender.Inapp{DB: s.DB, Cache: s.Cache}
			if e = n.Send(ctx, model.Channel{}, model.Delivery{UserID: u.ID, Title: "上游自动登录已停止", Content: fmt.Sprintf("上游账号 #%d 连续登录失败三次，请管理员检查并手动重试。", id)}, 0); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Service) check(ctx context.Context, id uint) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	key := fmt.Sprintf("upstream-login:%d", id)
	lock, e := s.Cache.Lock(ctx, key, time.Minute)
	if e != nil {
		return nil, e
	}
	defer s.Cache.Unlock(context.Background(), key, lock)
	session, impl, e := s.Load(ctx, id)
	if e != nil {
		return nil, e
	}
	if session.Account.State == Online {
		e = impl.Check(ctx, session)
		if e == nil {
			if e = s.save(ctx, session, map[string]any{}); e != nil {
				return nil, e
			}
			return session, nil
		}
		if !errors.Is(e, ErrSession) {
			return nil, e
		}
		if e = s.state(ctx, id, Expired); e != nil {
			return nil, e
		}
	}
	return nil, ErrSession
}
func (s *Service) Ensure(ctx context.Context, id uint) (*Session, error) {
	session, e := s.check(ctx, id)
	if e == nil {
		return session, nil
	}
	if !errors.Is(e, ErrSession) {
		return nil, e
	}
	if e = s.Login(ctx, id, true); e != nil {
		return nil, e
	}
	session, _, e = s.Load(ctx, id)
	return session, e
}
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var accounts []model.UpstreamAccount
			_ = s.DB.WithContext(ctx).Model(&model.UpstreamAccount{}).Where("state IN ? AND updated_at < ?", []string{Sending, Waiting, LoggingIn}, time.Now().Add(-12*time.Minute)).Update("state", Expired).Error
			if s.DB.WithContext(ctx).Where("enabled = ? AND state IN ? AND failures < ?", true, []string{Online, Expired, Failed}, 3).Find(&accounts).Error != nil {
				continue
			}
			for _, a := range accounts {
				_, _ = s.Ensure(ctx, a.ID)
			}
		}
	}
}
