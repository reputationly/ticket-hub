package notifier

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/reputationly/ticket-hub/internal/model"
)

const DefaultTemplate = "【工单提醒】{{.Ticket.Title}}\n类型：{{.Ticket.Kind}}\n状态：{{.Ticket.Status}}\n机房：{{.Ticket.Site}}\n事件：{{.Event.Type}}\n{{.Link}}"

type TemplateData struct {
	Ticket model.Ticket
	Event  model.TicketEvent
	Link   string
}
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return 0, errors.New("模板结果不能超过 64KB")
	}
	return b.Buffer.Write(p)
}
func Render(source string, t model.Ticket, e model.TicketEvent, publicURL string) (string, error) {
	if source == "" {
		source = DefaultTemplate
	}
	if len(source) > 16<<10 {
		return "", errors.New("模板不能超过 16KB")
	}
	tpl, err := template.New("提醒").Option("missingkey=error").Parse(source)
	if err != nil {
		return "", errors.New("模板语法错误")
	}
	var out limitedBuffer
	err = tpl.Execute(&out, TemplateData{t, e, strings.TrimRight(publicURL, "/") + fmt.Sprintf("/tickets/%d", t.ID)})
	if err != nil {
		return "", errors.New("模板字段或输出错误")
	}
	return out.String(), nil
}
func includes(list []string, value string) bool {
	if len(list) == 0 {
		return true
	}
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
func Matches(c model.Conditions, t model.Ticket, e model.TicketEvent) bool {
	return includes(c.Kinds, t.Kind) && includes(c.Statuses, t.Status) && includes(c.Sites, t.Site) && includes(c.EventTypes, e.Type) && (c.Keyword == "" || strings.Contains(strings.ToLower(t.Title+" "+t.UpstreamID), strings.ToLower(c.Keyword)))
}
func Urgent(t model.Ticket) bool {
	switch strings.ToLower(t.Priority) {
	case "urgent", "critical", "p0", "p1", "紧急":
		return true
	}
	return false
}
func Quiet(s model.Subscription, now time.Time, urgent bool) bool {
	if s.QuietStart == "" || s.QuietEnd == "" || urgent && s.UrgentBypass {
		return false
	}
	loc := time.Local
	if s.Timezone != "" {
		l, e := time.LoadLocation(s.Timezone)
		if e == nil {
			loc = l
		}
	}
	minute := now.In(loc).Format("15:04")
	if s.QuietStart == s.QuietEnd {
		return true
	}
	if s.QuietStart < s.QuietEnd {
		return minute >= s.QuietStart && minute < s.QuietEnd
	}
	return minute >= s.QuietStart || minute < s.QuietEnd
}
func ValidateSubscription(s model.Subscription) error {
	if (s.QuietStart == "") != (s.QuietEnd == "") {
		return errors.New("免打扰开始和结束必须同时填写")
	}
	for _, v := range []string{s.QuietStart, s.QuietEnd} {
		if v != "" {
			if parsed, e := time.Parse("15:04", v); e != nil || parsed.Format("15:04") != v {
				return errors.New("免打扰时间格式必须为 HH:MM")
			}
		}
	}
	if s.Timezone != "" {
		if _, e := time.LoadLocation(s.Timezone); e != nil {
			return errors.New("时区不正确")
		}
	}
	return nil
}
