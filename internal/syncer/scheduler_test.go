package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/collector"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/db"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/notifier"
	"github.com/reputationly/ticket-hub/internal/queue"
	"github.com/reputationly/ticket-hub/internal/sender"
	"github.com/reputationly/ticket-hub/internal/ticket"
	"github.com/reputationly/ticket-hub/internal/upstream"
)

func fixture(t *testing.T) (*Scheduler, *notifier.Service) {
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
	notify := notifier.New(d, store, cipher, q, sender.New(d, store, sender.Options{}))
	ts := &ticket.Service{DB: d, Cache: store, Dispatcher: notify}
	if e = collector.Seed(d); e != nil {
		t.Fatal(e)
	}
	return New(d, store, collector.NewRegistry(), nil, ts, true), notify
}
func TestMockEndToEndAndDebounce(t *testing.T) {
	s, n := fixture(t)
	ctx := context.Background()
	if e := s.DB.Create(&model.User{ID: 1, Username: "admin", PasswordHash: "test", Enabled: true}).Error; e != nil {
		t.Fatal(e)
	}
	if e := s.DB.Create(&model.NotifyRule{Name: "所有工单", Enabled: true, Actions: model.RuleActions{UserIDs: []uint{1}, PersonalChannels: []string{"inapp"}}}).Error; e != nil {
		t.Fatal(e)
	}
	s.DB.Model(&model.Collector{}).Where("kind = ?", "fault_repair").Updates(map[string]any{"enabled": true, "mock": true})
	for i := 0; i < 4; i++ {
		if e := s.Trigger(ctx, "fault_repair", "mail"); e != nil {
			t.Fatal(e)
		}
	}
	if len(s.requests) != 1 {
		t.Fatal("未去抖", len(s.requests))
	}
	req := <-s.requests
	log, e := s.Sync(ctx, req.kind, req.source)
	if e != nil || log.Count != 1 || log.Created != 1 || log.FinishedAt == nil {
		t.Fatal(log, e)
	}
	var jobs []model.NotifyJob
	s.DB.Find(&jobs)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	if e = n.Process(ctx, jobs[0].ID); e != nil {
		t.Fatal(e)
	}
	var notes []model.Notification
	s.DB.Find(&notes)
	if len(notes) != 1 || !strings.Contains(notes[0].Title, "故障维修") {
		t.Fatal(notes)
	}
	var tickets []model.Ticket
	s.DB.Find(&tickets)
	if len(tickets) != 1 || tickets[0].Kind != "fault_repair" {
		t.Fatal(tickets)
	}
	token, e := s.Cache.Lock(ctx, "sync:fault_repair", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Sync(ctx, "fault_repair", "manual"); !errors.Is(e, cache.ErrLock) {
		t.Fatal("执行锁失效", e)
	}
	s.Cache.Unlock(ctx, "sync:fault_repair", token)
	if e = s.Trigger(ctx, "missing", "manual"); e == nil {
		t.Fatal("未知采集器未拒绝")
	}
	t.Log("mock Collector → Upsert → 站内通知，邮件/手动共用触发去抖与执行锁通过")
}

type fakeAccounts struct{ logins int }

func (a *fakeAccounts) Ensure(context.Context, uint) (*upstream.Session, error) {
	return &upstream.Session{}, nil
}
func (a *fakeAccounts) Login(context.Context, uint, bool) error { a.logins++; return nil }

type fakeCollector struct {
	collector.Stub
	calls   int
	err     error
	expired bool
}

func (f *fakeCollector) List(context.Context, *upstream.Session, time.Time) ([]collector.RawTicket, error) {
	f.calls++
	if f.expired && f.calls == 1 {
		return nil, upstream.ErrSession
	}
	return nil, f.err
}
func TestBackoffAndRelogin(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	accounts := &fakeAccounts{}
	s.Upstream = accounts
	impl := &fakeCollector{Stub: collector.Stub{Type: "fault_repair"}, expired: true}
	s.Registry.Register(impl)
	s.DB.Model(&model.Collector{}).Where("kind = ?", "fault_repair").Updates(map[string]any{"enabled": true, "upstream_account_id": 1})
	log, e := s.Sync(ctx, "fault_repair", "manual")
	if e != nil || accounts.logins != 1 || impl.calls != 2 || log.Count != 0 {
		t.Fatal(log, e, accounts, impl.calls)
	}
	impl.err = errors.New("upstream unavailable")
	for i := 1; i <= 3; i++ {
		_, e = s.Sync(ctx, "fault_repair", "manual")
		if e == nil {
			t.Fatal("错误未传播")
		}
		var cfg model.Collector
		s.DB.First(&cfg, "kind = ?", "fault_repair")
		if cfg.Failures != i || cfg.LastError == "" || cfg.NextRunAt == nil {
			t.Fatal(cfg)
		}
		before := impl.calls
		if _, e = s.Sync(ctx, "fault_repair", "mail"); e == nil || impl.calls != before {
			t.Fatal("退避期间仍请求", e)
		}
		now = *cfg.NextRunAt
	}
	if Backoff(1) != 10*time.Second || Backoff(3) != 40*time.Second || Backoff(20) != 10*time.Minute {
		t.Fatal("退避间隔错误")
	}
	s.DB.Model(&model.Collector{}).Where("kind = ?", "security").Update("enabled", true)
	log, e = s.Sync(ctx, "security", "manual")
	if !errors.Is(e, collector.ErrUnimplemented) || log.Error != "未实现，待抓包" {
		t.Fatal(log, e)
	}
}
func TestIntervalsAndValidation(t *testing.T) {
	cfg := model.Collector{Kind: "fault_repair", IntervalSeconds: 30, WorkIntervalSeconds: 10, OffIntervalSeconds: 60, WorkStart: "22:00", WorkEnd: "06:00", Timezone: "UTC"}
	if e := Validate(cfg, false); e != nil {
		t.Fatal(e)
	}
	if Interval(cfg, time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)) != 10*time.Second || Interval(cfg, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) != time.Minute {
		t.Fatal("跨午夜工作时间错误")
	}
	cfg.Mock = true
	if Validate(cfg, false) == nil {
		t.Fatal("生产允许模拟")
	}
	cfg.Mock = false
	cfg.IntervalSeconds = 1
	if Validate(cfg, true) == nil {
		t.Fatal("非法间隔")
	}
}
