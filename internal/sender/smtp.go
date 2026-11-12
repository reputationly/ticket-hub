package sender

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"html/template"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/reputationly/ticket-hub/internal/model"
)

type TLSConfig = *tls.Config
type SMTP struct{ TLSConfig *tls.Config }

const defaultHTML = "<h3>{{.Title}}</h3><div style=\"white-space:pre-wrap\">{{.Content}}</div>"

func (s *SMTP) Send(ctx context.Context, ch model.Channel, d model.Delivery, _ uint) error {
	from, e := mail.ParseAddress(Text(ch.Config, "from"))
	if e != nil {
		return errors.New("发件人格式错误")
	}
	to, e := mail.ParseAddress(d.Email)
	if e != nil {
		return errors.New("收件人格式错误")
	}
	html := Text(ch.Config, "html_template")
	if html == "" {
		html = defaultHTML
	}
	tpl, e := template.New("邮件").Option("missingkey=error").Parse(html)
	if e != nil {
		return errors.New("邮件模板错误")
	}
	var body bytes.Buffer
	if e = tpl.Execute(&body, d); e != nil {
		return errors.New("邮件模板渲染失败")
	}
	host := Text(ch.Config, "host")
	addr := net.JoinHostPort(host, Text(ch.Config, "port"))
	tc := s.TLSConfig
	if tc == nil {
		tc = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	} else {
		tc = tc.Clone()
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, e := dialer.DialContext(ctx, "tcp", addr)
	if e != nil {
		return errors.New("SMTP 连接失败")
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if Text(ch.Config, "tls") == "ssl" {
		secure := tls.Client(conn, tc)
		if e = secure.HandshakeContext(ctx); e != nil {
			return errors.New("SMTP TLS 握手失败")
		}
		conn = secure
	}
	client, e := smtp.NewClient(conn, host)
	if e != nil {
		return errors.New("SMTP 握手失败")
	}
	defer client.Close()
	switch Text(ch.Config, "tls") {
	case "starttls":
		if e = client.StartTLS(tc); e != nil {
			return errors.New("SMTP STARTTLS 失败")
		}
	case "ssl", "none":
	default:
		return errors.New("SMTP TLS 模式错误")
	}
	if user := Text(ch.Config, "username"); user != "" {
		if e = client.Auth(smtp.PlainAuth("", user, Text(ch.Config, "password"), host)); e != nil {
			return errors.New("SMTP 认证失败")
		}
	}
	if e = client.Mail(from.Address); e != nil {
		return errors.New("SMTP 发件人被拒绝")
	}
	if e = client.Rcpt(to.Address); e != nil {
		return errors.New("SMTP 收件人被拒绝")
	}
	w, e := client.Data()
	if e != nil {
		return errors.New("SMTP DATA 失败")
	}
	message := "From: " + from.String() + "\r\nTo: " + to.String() + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", strings.ReplaceAll(strings.ReplaceAll(d.Title, "\r", ""), "\n", "")) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n"
	encoded := base64.StdEncoding.EncodeToString(body.Bytes())
	for len(encoded) > 76 {
		message += encoded[:76] + "\r\n"
		encoded = encoded[76:]
	}
	message += encoded + "\r\n"
	if _, e = w.Write([]byte(message)); e != nil {
		return errors.New("SMTP 写入失败")
	}
	if e = w.Close(); e != nil {
		return errors.New("SMTP 投递失败")
	}
	if e = client.Quit(); e != nil {
		return errors.New("SMTP 退出失败")
	}
	return nil
}
