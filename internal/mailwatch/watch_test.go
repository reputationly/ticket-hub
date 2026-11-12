package mailwatch

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/db"
	"github.com/reputationly/ticket-hub/internal/model"
)

type idleTrace struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	ready  chan struct{}
}

func (t *idleTrace) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buffer.Write(p)
	if strings.Contains(t.buffer.String(), " IDLE\r\n") {
		select {
		case t.ready <- struct{}{}:
		default:
		}
		t.buffer.Reset()
	}
	return len(p), nil
}

type updatingBackend struct {
	backend.Backend
	updates chan backend.Update
}

func (b *updatingBackend) Updates() <-chan backend.Update { return b.updates }
func TestIDLECodeAndTrigger(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d, e := db.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := d.DB()
	defer sql.Close()
	cipher, _ := crypto.New(strings.Repeat("s", 32))
	encrypted, _ := cipher.Encrypt("password")
	b := &updatingBackend{memory.New(), make(chan backend.Update, 16)}
	srv := server.New(b)
	srv.AllowInsecureAuth = true
	ready := make(chan struct{}, 16)
	srv.Debug = imap.NewDebugWriter(io.Discard, &idleTrace{ready: ready})
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	go srv.Serve(listener)
	defer srv.Close()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	a := model.MailAccount{ID: 1, Host: host, Port: number, TLS: "none", Username: "username", Password: encrypted, Enabled: true}
	h := New(d, cipher)
	h.Poll = time.Hour
	triggered := make(chan string, 1)
	h.Trigger = func(ctx context.Context, kind string) error { triggered <- kind; return nil }
	if e = d.Create(&model.MailRule{Name: "通知", MailAccountID: 1, Enabled: true, From: "sender@example.com", Subject: "验证码", Kind: "fault_repair"}).Error; e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { defer close(done); h.Watch(ctx, a) }()
	defer func() { cancel(); <-done }()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("未进入 IDLE")
	}
	codeResult := make(chan string, 1)
	since := time.Now().Add(-time.Second)
	go func() {
		code, e := h.WaitCode(ctx, 1, Filter{From: "sender@example.com", Subject: "验证码", Since: since})
		if e != nil {
			codeResult <- "error: " + e.Error()
		} else {
			codeResult <- code
		}
	}()
	user, e := b.Login(nil, "username", "password")
	if e != nil {
		t.Fatal(e)
	}
	box, e := user.GetMailbox("INBOX")
	if e != nil {
		t.Fatal(e)
	}
	body := "From: sender@example.com\r\nSubject: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("登录验证码")) + "?=\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte("<html><script>000000</script><p>验证码：654321</p></html>"))
	if e = box.CreateMessage(nil, time.Now(), strings.NewReader(body)); e != nil {
		t.Fatal(e)
	}
	status, _ := box.Status([]imap.StatusItem{imap.StatusMessages})
	update := &backend.MailboxUpdate{Update: backend.NewUpdate("username", "INBOX"), MailboxStatus: status}
	b.updates <- update
	select {
	case code := <-codeResult:
		if code != "654321" {
			t.Fatal(code)
		}
	case <-ctx.Done():
		t.Fatal("IDLE 未触发提码")
	}
	select {
	case kind := <-triggered:
		if kind != "fault_repair" {
			t.Fatal(kind)
		}
	case <-ctx.Done():
		t.Fatal("未触发采集器")
	}
	count, e := h.Test(ctx, a)
	if e != nil || count != 2 {
		t.Fatal(count, e)
	}
	t.Log("内存 IMAP：IDLE 新信推送、中文主题、HTML/base64 提码和邮件规则触发通过")
}
func TestMIMEAndFilters(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct{ encoding, body string }{{"base64", base64.StdEncoding.EncodeToString([]byte("验证码 123456"))}, {"quoted-printable", "=E9=AA=8C=E8=AF=81=E7=A0=81 123456"}} {
		raw := fmt.Sprintf("From: sender@example.com\r\nSubject: =?UTF-8?B?55m75b2V?=\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: %s\r\n\r\n%s", tc.encoding, tc.body)
		m, e := Parse(strings.NewReader(raw), now)
		if e != nil || m.Subject != "登录" {
			t.Fatal(m, e)
		}
		code, ok := Extract(m, Filter{From: "sender@", Subject: "登录", Since: now.Add(-time.Second)})
		if !ok || code != "123456" {
			t.Fatal(code, ok)
		}
		if _, ok = Extract(m, Filter{Since: now.Add(time.Second)}); ok {
			t.Fatal("旧邮件被匹配")
		}
		if _, ok = Extract(m, Filter{Subject: "不存在"}); ok {
			t.Fatal("主题过滤失败")
		}
	}
	if e := Validate(model.CodeFilter{From: "["}); e == nil {
		t.Fatal("错误正则未拒绝")
	}
	if _, ok := Extract(Message{Text: "code:ABC", Date: now}, Filter{Code: `code:(\w+)`}); !ok {
		t.Fatal("自定义捕获组失败")
	}
	if text := HTMLText("<style>111111</style><p>hello &amp; world</p>"); text != "hello & world" {
		t.Fatal(text)
	}
	h := New(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := h.WaitCode(ctx, 1, Filter{}); e == nil {
		t.Fatal("取消未传播")
	}
	if _, e := Parse(strings.NewReader("invalid\x00header"), now); e == nil {
		t.Fatal("无效 MIME 未拒绝")
	}
}
