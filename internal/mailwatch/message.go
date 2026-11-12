package mailwatch

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"github.com/reputationly/ticket-hub/internal/model"
	"golang.org/x/net/html"
)

type Message struct {
	UID                 uint32
	From, Subject, Text string
	Date                time.Time
}
type Filter struct {
	From, Subject, Code string
	Since               time.Time
}

func Validate(f model.CodeFilter) error {
	for _, p := range []string{f.From, f.Subject, f.Code} {
		if len(p) > 2048 {
			return errors.New("正则不能超过 2048 字节")
		}
		if _, e := regexp.Compile(p); e != nil {
			return errors.New("正则表达式错误")
		}
	}
	return nil
}
func Match(m Message, from, subject string) bool {
	a, e := regexp.Compile(from)
	if e != nil {
		return false
	}
	b, e := regexp.Compile(subject)
	return e == nil && a.MatchString(m.From) && b.MatchString(m.Subject)
}
func Extract(m Message, f Filter) (string, bool) {
	if m.Date.Before(f.Since) || !Match(m, f.From, f.Subject) {
		return "", false
	}
	p := f.Code
	if p == "" {
		p = `\b\d{6}\b`
	}
	r, e := regexp.Compile(p)
	if e != nil {
		return "", false
	}
	matches := r.FindStringSubmatch(m.Text)
	if len(matches) == 0 {
		return "", false
	}
	if len(matches) > 1 {
		return matches[1], true
	}
	return matches[0], true
}
func Parse(r io.Reader, date time.Time) (Message, error) {
	mr, e := mail.CreateReader(io.LimitReader(r, 4<<20))
	if e != nil {
		return Message{}, errors.New("邮件 MIME 解析失败")
	}
	defer mr.Close()
	subject, e := mr.Header.Subject()
	if e != nil {
		return Message{}, errors.New("邮件主题解码失败")
	}
	m := Message{From: mr.Header.Get("From"), Subject: subject, Date: date}
	for {
		p, e := mr.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return m, errors.New("邮件正文解析失败")
		}
		header, ok := p.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		typ, _, e := header.ContentType()
		if e != nil {
			return m, e
		}
		body, e := io.ReadAll(io.LimitReader(p.Body, 2<<20))
		if e != nil {
			return m, e
		}
		if typ == "text/html" {
			m.Text += "\n" + HTMLText(string(body))
		} else if typ == "text/plain" {
			m.Text += "\n" + string(body)
		}
	}
	return m, nil
}
func HTMLText(source string) string {
	root, e := html.Parse(strings.NewReader(source))
	if e != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return strings.Join(strings.Fields(b.String()), " ")
}
