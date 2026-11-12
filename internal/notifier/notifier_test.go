package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/db"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/queue"
	"github.com/reputationly/ticket-hub/internal/sender"
	"github.com/reputationly/ticket-hub/internal/ticket"
	"gorm.io/gorm"
)

func setup(t *testing.T) (*Service, *queue.Memory) {
	t.Helper()
	d, e := db.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := d.DB()
	t.Cleanup(func() { sql.Close() })
	store := cache.NewMemory()
	t.Cleanup(func() { store.Close() })
	cipher, _ := crypto.New(strings.Repeat("s", 32))
	q := queue.NewMemory()
	t.Cleanup(func() { q.Close() })
	s := New(d, store, cipher, q, sender.New(d, store, sender.Options{}))
	return s, q
}
func create(t *testing.T, d *gorm.DB, rows ...any) {
	t.Helper()
	for _, row := range rows {
		if e := d.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
}
func event(t *testing.T, s *Service, input model.Ticket) *model.TicketEvent {
	t.Helper()
	ts := &ticket.Service{DB: s.DB, Cache: s.Cache}
	_, ev, e := ts.Upsert(context.Background(), input)
	if e != nil {
		t.Fatal(e)
	}
	return ev
}

func testDispatch(t *testing.T, s *Service) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 23, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	s.PublicURL = func(context.Context) (string, error) { return "https://tickets.example", nil }
	u1 := model.User{Username: "quiet", PasswordHash: "test", Enabled: true, Phone: "13800000001", Email: "quiet@example.com"}
	u2 := model.User{Username: "active", PasswordHash: "test", Enabled: true, Phone: "13800000002", Email: "active@example.com", FeishuOpenID: "ou_test"}
	create(t, s.DB, &u1, &u2)
	email := model.Channel{Type: "email", Name: "邮件", Enabled: true}
	bot := model.Channel{Type: "wecom_bot", Name: "群", Enabled: true}
	create(t, s.DB, &email, &bot)
	sub1 := model.Subscription{UserID: u1.ID, Enabled: true, Kinds: []string{"repair"}, Channels: []string{"email"}, QuietStart: "22:00", QuietEnd: "07:00", Timezone: "UTC", MentionInGroup: true}
	sub2 := model.Subscription{UserID: u2.ID, Enabled: true, Kinds: []string{"repair"}, Channels: []string{"email"}, MentionInGroup: true}
	create(t, s.DB, &sub1, &sub2)
	for i := 0; i < 2; i++ {
		create(t, s.DB, &model.NotifyRule{Name: "规则", Enabled: true, Conditions: model.Conditions{Kinds: []string{"repair"}, Statuses: []string{"待领"}, Sites: []string{"北京"}, Keyword: "设备", EventTypes: []string{"ticket.created"}}, Actions: model.RuleActions{UserIDs: []uint{u1.ID, u2.ID}, GroupChannelIDs: []uint{bot.ID, bot.ID}, PersonalChannels: []string{"email"}, Template: "{{.Ticket.Title}} {{.Link}}"}})
	}
	ev := event(t, s, model.Ticket{Kind: "repair", UpstreamID: "A", Title: "设备维修", Status: "待领", Site: "北京"})
	if e := s.Dispatch(ctx, ev.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.Dispatch(ctx, ev.ID); e != nil {
		t.Fatal(e)
	}
	var jobs []model.NotifyJob
	if e := s.DB.Where("event_id = ?", ev.ID).Find(&jobs).Error; e != nil {
		t.Fatal(e)
	}
	if len(jobs) != 4 {
		t.Fatalf("去重及免打扰结果错误：%+v", jobs)
	}
	for _, job := range jobs {
		if job.UserID == u1.ID && job.ChannelType != "inapp" {
			t.Fatal("免打扰仍发外部消息")
		}
		if job.ChannelID == bot.ID {
			if len(job.Delivery.MentionMobiles) != 1 || job.Delivery.MentionMobiles[0] != u2.Phone {
				t.Fatal(job.Delivery)
			}
		}
		if !strings.Contains(job.Delivery.Content, "/tickets/") {
			t.Fatal(job.Delivery.Content)
		}
	}
	quietCount := 0
	for _, job := range jobs {
		if job.UserID == u1.ID && job.ChannelType == "inapp" {
			quietCount++
		}
	}
	if quietCount != 1 {
		t.Fatal("站内兜底不应被免打扰丢弃")
	}
	ev2 := event(t, s, model.Ticket{Kind: "unmatched", UpstreamID: "B", Title: "无匹配"})
	if e := s.Dispatch(ctx, ev2.ID); e != nil {
		t.Fatal(e)
	}
	var n int64
	s.DB.Model(&model.NotifyJob{}).Where("event_id = ?", ev2.ID).Count(&n)
	if n != 0 {
		t.Fatal(n)
	}
	t.Log("规则匹配、订阅合并、用户/渠道与群去重、免打扰和站内兜底验证完成")
}
func TestDispatch(t *testing.T) { s, _ := setup(t); testDispatch(t, s) }
func TestPGDispatch(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_PG_DSN")
	}
	d, e := db.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := d.DB()
	defer sql.Close()
	tx := d.Begin()
	defer tx.Rollback()
	store := cache.NewMemory()
	defer store.Close()
	cipher, _ := crypto.New(strings.Repeat("s", 32))
	q := queue.NewMemory()
	defer q.Close()
	s := New(tx, store, cipher, q, nil)
	testDispatch(t, s)
}
func TestQuietAndMatch(t *testing.T) {
	sub := model.Subscription{QuietStart: "22:00", QuietEnd: "07:00", Timezone: "Asia/Shanghai"}
	for _, tc := range []struct {
		hour, minute int
		want         bool
	}{{21, 59, false}, {22, 0, true}, {23, 0, true}, {0, 0, true}, {6, 59, true}, {7, 0, false}, {12, 0, false}} {
		loc, _ := time.LoadLocation("Asia/Shanghai")
		if got := Quiet(sub, time.Date(2026, 1, 1, tc.hour, tc.minute, 0, 0, loc), false); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	sub.UrgentBypass = true
	if Quiet(sub, time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC), true) {
		t.Fatal("紧急突破失败")
	}
	sub.QuietStart = ""
	sub.QuietEnd = ""
	if Quiet(sub, time.Now(), false) {
		t.Fatal("空时段不应免打扰")
	}
	for _, invalid := range []model.Subscription{{QuietStart: "22:00"}, {QuietStart: "25:00", QuietEnd: "07:00"}, {Timezone: "bad/zone"}} {
		if ValidateSubscription(invalid) == nil {
			t.Fatal(invalid)
		}
	}
	ticket := model.Ticket{Kind: "repair", Status: "待领", Site: "北京", Title: "测试工单"}
	ev := model.TicketEvent{Type: "ticket.created"}
	for _, condition := range []model.Conditions{{Kinds: []string{"other"}}, {Statuses: []string{"完成"}}, {Sites: []string{"上海"}}, {Keyword: "不存在"}, {EventTypes: []string{"ticket.updated"}}} {
		if Matches(condition, ticket, ev) {
			t.Fatal(condition)
		}
	}
	if !Matches(model.Conditions{}, ticket, ev) {
		t.Fatal("空条件应匹配")
	}
}
func TestTemplate(t *testing.T) {
	ticket := model.Ticket{ID: 9, Title: "工单", Status: "待领"}
	out, e := Render("", ticket, model.TicketEvent{Type: "ticket.created"}, "https://example.com/")
	if e != nil || !strings.Contains(out, "https://example.com/tickets/9") || !strings.Contains(out, "工单提醒") {
		t.Fatal(out, e)
	}
	for _, source := range []string{"{{", "{{.Missing}}", strings.Repeat("字", 20000)} {
		if _, e = Render(source, ticket, model.TicketEvent{}, ""); e == nil {
			t.Fatal("模板错误未返回")
		}
	}
}
func TestWorkerRetryRateAndRecovery(t *testing.T) {
	s, q := setup(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	ch, e := sender.Prepare(s.Cipher, model.Channel{Type: "sms_aliyun", Name: "短信", Enabled: true, Config: map[string]any{"access_key_id": "local-id", "access_key_secret": "local-secret", "sign_name": "测试", "template_code": "SMS_1"}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	create(t, s.DB, &ch)
	calls := 0
	s.Senders["sms_aliyun"] = sender.Func(func(context.Context, model.Channel, model.Delivery, uint) error { calls++; return nil })
	jobs := []model.NotifyJob{{DedupeKey: "rate1", ChannelID: ch.ID, ChannelType: ch.Type, Delivery: model.Delivery{Phone: "13800000000"}, Status: "pending", NextAttemptAt: now}, {DedupeKey: "rate2", ChannelID: ch.ID, ChannelType: ch.Type, Delivery: model.Delivery{Phone: "13800000000"}, Status: "pending", NextAttemptAt: now}}
	create(t, s.DB, &jobs)
	for _, job := range jobs {
		if e = s.Process(ctx, job.ID); e != nil {
			t.Fatal(e)
		}
	}
	var delayed model.NotifyJob
	s.DB.First(&delayed, jobs[1].ID)
	if delayed.Status != "pending" || delayed.Retries != 0 || !delayed.NextAttemptAt.Equal(now.Add(time.Minute)) || calls != 1 {
		t.Fatal(delayed, calls)
	}
	now = now.Add(time.Minute)
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	readCtx, readCancel := context.WithTimeout(ctx, time.Second)
	queued, e := q.Pop(readCtx)
	readCancel()
	if e != nil || queued.JobID != delayed.ID {
		t.Fatal("限频任务未重新入队", queued, e)
	}
	if e = s.Process(ctx, delayed.ID); e != nil {
		t.Fatal(e)
	}
	s.DB.First(&delayed, delayed.ID)
	if delayed.Status != "sent" || calls != 2 {
		t.Fatal(delayed, calls)
	}
	if e = s.Process(ctx, delayed.ID); e != nil || calls != 2 {
		t.Fatal("重复队列消息重复发送", e, calls)
	}
	inapp := model.NotifyJob{DedupeKey: "retry", ChannelType: "inapp", Delivery: model.Delivery{UserID: 1}, Status: "pending", NextAttemptAt: now}
	create(t, s.DB, &inapp)
	s.Senders["inapp"] = sender.Func(func(context.Context, model.Channel, model.Delivery, uint) error {
		return errors.New("本地模拟失败")
	})
	for i := 0; i < 5; i++ {
		if e = s.Process(ctx, inapp.ID); e != nil {
			t.Fatal(e)
		}
		s.DB.First(&inapp, inapp.ID)
		if i < 4 {
			if inapp.Status != "pending" || inapp.Retries != i+1 || !inapp.NextAttemptAt.Equal(now.Add(RetryDelays[i])) {
				t.Fatal(i, inapp)
			}
			now = inapp.NextAttemptAt
		} else if inapp.Status != "failed" || inapp.Attempts != 5 || inapp.LastError != "本地模拟失败" {
			t.Fatal(inapp)
		}
	}
	expired := now.Add(-time.Minute)
	pending := model.NotifyJob{DedupeKey: "recover", ChannelType: "inapp", Status: "processing", LeaseUntil: &expired, NextAttemptAt: now}
	create(t, s.DB, &pending)
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	item, e := q.Pop(deadline)
	if e != nil || item.JobID != pending.ID {
		t.Fatal(item, e)
	}
	s.Senders["inapp"] = sender.Func(func(context.Context, model.Channel, model.Delivery, uint) error { return nil })
	if e = s.Process(ctx, item.JobID); e != nil {
		t.Fatal(e)
	}
	s.DB.First(&pending, pending.ID)
	if pending.Status != "sent" {
		t.Fatal(pending)
	}
	if e = s.Resend(ctx, inapp.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Process(ctx, inapp.ID); e != nil {
		t.Fatal(e)
	}
	s.DB.First(&inapp, inapp.ID)
	if inapp.Status != "sent" {
		t.Fatal(inapp)
	}
	t.Log("短信限频不丢弃、延迟重入队、四次重试、过期任务恢复与重发验证完成")
}
func TestRateBots(t *testing.T) {
	s, _ := setup(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		kind  string
		count int
	}{{"wecom_bot", 20}, {"feishu_bot", 5}} {
		ch := model.Channel{Type: tc.kind, Config: map[string]any{"webhook": "https://example.com/" + tc.kind}}
		for i := 0; i < tc.count; i++ {
			if delay, e := s.Allow(context.Background(), ch, model.Delivery{}, now); e != nil || delay != 0 {
				t.Fatal(delay, e)
			}
		}
		if delay, e := s.Allow(context.Background(), ch, model.Delivery{}, now); e != nil || delay <= 0 {
			t.Fatal(delay, e)
		}
	}
}
func TestDispatchTemplateFailure(t *testing.T) {
	s, _ := setup(t)
	create(t, s.DB, &model.User{ID: 1, Username: "u", PasswordHash: "test", Enabled: true}, &model.NotifyRule{Name: "坏模板", Enabled: true, Actions: model.RuleActions{UserIDs: []uint{1}, Template: "{{.Missing}}"}})
	ev := event(t, s, model.Ticket{Kind: "repair", UpstreamID: "bad", Title: "工单"})
	if e := s.Dispatch(context.Background(), ev.ID); e == nil {
		t.Fatal("模板错误未传播")
	}
	var persisted model.TicketEvent
	s.DB.First(&persisted, ev.ID)
	var n int64
	s.DB.Model(&model.NotifyJob{}).Count(&n)
	if persisted.Dispatched || n != 0 {
		data, _ := json.Marshal(persisted)
		t.Fatal(string(data), n)
	}
}
