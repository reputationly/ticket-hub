package mailwatch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/model"
	"gorm.io/gorm"
)

type Hub struct {
	DB        *gorm.DB
	Cipher    *crypto.Cipher
	Trigger   func(context.Context, string) error
	Poll      time.Duration
	TLSConfig *tls.Config
	mu        sync.Mutex
	history   map[uint][]Message
	waiters   map[uint]map[uint]chan Message
	next      uint
}

func New(d *gorm.DB, c *crypto.Cipher) *Hub {
	return &Hub{DB: d, Cipher: c, Poll: 15 * time.Second, history: map[uint][]Message{}, waiters: map[uint]map[uint]chan Message{}}
}
func Dial(ctx context.Context, a model.MailAccount, tc *tls.Config) (*client.Client, error) {
	addr := net.JoinHostPort(a.Host, strconv.Itoa(a.Port))
	if tc == nil {
		tc = &tls.Config{ServerName: a.Host, MinVersion: tls.VersionTLS12}
	}
	conn, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if e != nil {
		return nil, errors.New("IMAP 连接失败")
	}
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { rawConn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if a.TLS == "ssl" {
		secure := tls.Client(conn, tc)
		if e = secure.HandshakeContext(ctx); e != nil {
			conn.Close()
			return nil, errors.New("IMAP TLS 失败")
		}
		conn = secure
	}
	c, e := client.New(conn)
	if e != nil {
		conn.Close()
		return nil, errors.New("IMAP 握手失败")
	}
	c.Timeout = 15 * time.Second
	if a.TLS == "starttls" {
		if e = c.StartTLS(tc); e != nil {
			c.Terminate()
			return nil, errors.New("IMAP STARTTLS 失败")
		}
	}
	if e = c.Login(a.Username, a.Password); e != nil {
		c.Terminate()
		return nil, errors.New("IMAP 登录失败")
	}
	return c, nil
}
func (h *Hub) Test(ctx context.Context, a model.MailAccount) (uint32, error) {
	plain, e := h.Cipher.Decrypt(a.Password)
	if e != nil {
		return 0, errors.New("邮箱授权码解密失败")
	}
	a.Password = plain
	c, e := Dial(ctx, a, h.TLSConfig)
	if e != nil {
		return 0, e
	}
	defer c.Terminate()
	stop := context.AfterFunc(ctx, func() { c.Terminate() })
	defer stop()
	status, e := c.Select("INBOX", true)
	if e != nil {
		return 0, errors.New("无法读取 INBOX")
	}
	return status.Messages, nil
}
func (h *Hub) WaitCode(ctx context.Context, id uint, f Filter) (string, error) {
	if e := Validate(model.CodeFilter{From: f.From, Subject: f.Subject, Code: f.Code}); e != nil {
		return "", e
	}
	h.mu.Lock()
	h.next++
	key := h.next
	ch := make(chan Message, 256)
	if h.waiters[id] == nil {
		h.waiters[id] = map[uint]chan Message{}
	}
	h.waiters[id][key] = ch
	history := append([]Message(nil), h.history[id]...)
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.waiters[id], key); h.mu.Unlock() }()
	for _, m := range history {
		if code, ok := Extract(m, f); ok {
			return code, nil
		}
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case m := <-ch:
			if code, ok := Extract(m, f); ok {
				return code, nil
			}
		}
	}
}
func (h *Hub) deliver(ctx context.Context, id uint, m Message, fresh bool) {
	h.mu.Lock()
	history := append(h.history[id], m)
	if len(history) > 256 {
		history = history[len(history)-256:]
	}
	h.history[id] = history
	for _, ch := range h.waiters[id] {
		select {
		case ch <- m:
		default:
		}
	}
	h.mu.Unlock()
	if !fresh || h.Trigger == nil {
		return
	}
	var rules []model.MailRule
	if h.DB.WithContext(ctx).Where("mail_account_id = ? AND enabled = ?", id, true).Find(&rules).Error != nil {
		return
	}
	for _, r := range rules {
		if Match(m, r.From, r.Subject) {
			_ = h.Trigger(ctx, r.Kind)
		}
	}
}
func (h *Hub) Watch(ctx context.Context, a model.MailAccount) {
	var lastUID, validity uint32
	started := time.Now().Truncate(time.Second)
	backoff := time.Second
	for ctx.Err() == nil {
		connectedAt := time.Now()
		e := h.connection(ctx, a, &lastUID, &validity, started)
		if ctx.Err() != nil {
			return
		}
		if e == nil || time.Since(connectedAt) > time.Minute {
			backoff = time.Second
		}
		timer := time.NewTimer(backoff)
		backoff = min(backoff*2, time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (h *Hub) connection(ctx context.Context, a model.MailAccount, lastUID, validity *uint32, started time.Time) error {
	plain, e := h.Cipher.Decrypt(a.Password)
	if e != nil {
		return e
	}
	a.Password = plain
	c, e := Dial(ctx, a, h.TLSConfig)
	if e != nil {
		return e
	}
	defer c.Terminate()
	stop := context.AfterFunc(ctx, func() { c.Terminate() })
	defer stop()
	updates := make(chan client.Update, 64)
	c.Updates = updates
	status, e := c.Select("INBOX", true)
	if e != nil {
		return e
	}
	if *validity != status.UidValidity {
		*lastUID = 0
		*validity = status.UidValidity
	}
	scan := func() error {
		c.Timeout = 15 * time.Second
		criteria := imap.NewSearchCriteria()
		criteria.Since = time.Now().Add(-24 * time.Hour)
		uids, e := c.UidSearch(criteria)
		if e != nil {
			return e
		}
		if len(uids) > 500 {
			uids = uids[len(uids)-500:]
		}
		set := new(imap.SeqSet)
		for _, uid := range uids {
			if uid > *lastUID {
				set.AddNum(uid)
			}
		}
		if set.Empty() {
			return nil
		}
		section := &imap.BodySectionName{Peek: true}
		messages := make(chan *imap.Message, 16)
		done := make(chan error, 1)
		go func() {
			done <- c.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchInternalDate, section.FetchItem()}, messages)
		}()
		for msg := range messages {
			if body := msg.GetBody(section); body != nil {
				m, e := Parse(body, msg.InternalDate)
				if e == nil {
					m.UID = msg.Uid
					h.deliver(ctx, a.ID, m, !m.Date.Before(started))
				}
			}
			if msg.Uid > *lastUID {
				*lastUID = msg.Uid
			}
		}
		return <-done
	}
	if e = scan(); e != nil {
		return e
	}
	for {
		c.Timeout = 0
		idleStop := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- c.Idle(idleStop, &client.IdleOptions{PollInterval: h.Poll, LogoutTimeout: 25 * time.Minute})
		}()
		timer := time.NewTimer(h.Poll)
		select {
		case <-ctx.Done():
			close(idleStop)
			timer.Stop()
			<-done
			return ctx.Err()
		case e = <-done:
			timer.Stop()
			return e
		case <-updates:
		case <-timer.C:
		}
		timer.Stop()
		close(idleStop)
		if e = <-done; e != nil {
			return e
		}
		if e = scan(); e != nil {
			return e
		}
	}
}
func (h *Hub) Run(ctx context.Context) {
	type running struct {
		signature string
		cancel    context.CancelFunc
	}
	active := map[uint]running{}
	var wg sync.WaitGroup
	defer func() {
		for _, r := range active {
			r.cancel()
		}
		wg.Wait()
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var accounts []model.MailAccount
		if h.DB.WithContext(ctx).Where("enabled = ?", true).Find(&accounts).Error == nil {
			seen := map[uint]bool{}
			for _, a := range accounts {
				seen[a.ID] = true
				sig := fmt.Sprintf("%s:%d:%s:%s:%s", a.Host, a.Port, a.TLS, a.Username, a.Password)
				if old, ok := active[a.ID]; ok && old.signature == sig {
					continue
				}
				if old, ok := active[a.ID]; ok {
					old.cancel()
				}
				child, cancel := context.WithCancel(ctx)
				active[a.ID] = running{sig, cancel}
				wg.Add(1)
				go func(a model.MailAccount) { defer wg.Done(); h.Watch(child, a) }(a)
			}
			for id, r := range active {
				if !seen[id] {
					r.cancel()
					delete(active, id)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
