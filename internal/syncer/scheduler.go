package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/collector"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/ticket"
	"github.com/reputationly/ticket-hub/internal/upstream"
	"gorm.io/gorm"
)

type Accounts interface {
	Ensure(context.Context, uint) (*upstream.Session, error)
	Login(context.Context, uint, bool) error
}
type Scheduler struct {
	DB       *gorm.DB
	Cache    cache.Cache
	Registry *collector.Registry
	Upstream Accounts
	Tickets  *ticket.Service
	DevMode  bool
	Now      func() time.Time
	requests chan request
}
type request struct{ kind, source string }

func New(d *gorm.DB, c cache.Cache, r *collector.Registry, u Accounts, t *ticket.Service, dev bool) *Scheduler {
	return &Scheduler{DB: d, Cache: c, Registry: r, Upstream: u, Tickets: t, DevMode: dev, Now: time.Now, requests: make(chan request, 64)}
}

var ErrDebounced = errors.New("三秒内重复触发已合并")

func (s *Scheduler) Trigger(ctx context.Context, kind, source string) error {
	var cfg model.Collector
	if e := s.DB.WithContext(ctx).First(&cfg, "kind = ?", kind).Error; e != nil {
		return e
	}
	if !cfg.Enabled {
		return errors.New("采集器未启用")
	}
	token, e := s.Cache.Lock(ctx, "sync-debounce:"+kind, 3*time.Second)
	if errors.Is(e, cache.ErrLock) {
		return nil
	}
	if e != nil {
		return e
	}
	select {
	case s.requests <- request{kind, source}:
		return nil
	case <-ctx.Done():
		_ = s.Cache.Unlock(context.Background(), "sync-debounce:"+kind, token)
		return ctx.Err()
	default:
		_ = s.Cache.Unlock(context.Background(), "sync-debounce:"+kind, token)
		return errors.New("同步队列繁忙，请重试")
	}
}
func Interval(cfg model.Collector, now time.Time) time.Duration {
	seconds := cfg.IntervalSeconds
	if cfg.WorkStart != "" && cfg.WorkEnd != "" {
		loc, e := time.LoadLocation(cfg.Timezone)
		if e != nil {
			loc = time.Local
		}
		clock := now.In(loc).Format("15:04")
		work := clock >= cfg.WorkStart && clock < cfg.WorkEnd
		if cfg.WorkStart > cfg.WorkEnd {
			work = clock >= cfg.WorkStart || clock < cfg.WorkEnd
		}
		if work && cfg.WorkIntervalSeconds > 0 {
			seconds = cfg.WorkIntervalSeconds
		} else if !work && cfg.OffIntervalSeconds > 0 {
			seconds = cfg.OffIntervalSeconds
		}
	}
	if seconds < 10 {
		seconds = 10
	}
	if seconds > 600 {
		seconds = 600
	}
	return time.Duration(seconds) * time.Second
}
func Backoff(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	n := failures - 1
	if n > 6 {
		n = 6
	}
	delay := time.Duration(1<<n) * 10 * time.Second
	if delay > 10*time.Minute {
		delay = 10 * time.Minute
	}
	return delay
}
func (s *Scheduler) Sync(ctx context.Context, kind, source string) (model.SyncLog, error) {
	ctx, cancel := context.WithTimeout(ctx, 11*time.Minute)
	defer cancel()
	token, e := s.Cache.Lock(ctx, "sync:"+kind, 12*time.Minute)
	if e != nil {
		return model.SyncLog{}, e
	}
	defer s.Cache.Unlock(context.Background(), "sync:"+kind, token)
	var cfg model.Collector
	if e = s.DB.WithContext(ctx).First(&cfg, "kind = ?", kind).Error; e != nil {
		return model.SyncLog{}, e
	}
	if !cfg.Enabled {
		return model.SyncLog{}, errors.New("采集器未启用")
	}
	now := s.Now()
	if cfg.NextRunAt != nil && now.Before(*cfg.NextRunAt) {
		if cfg.Failures > 0 {
			return model.SyncLog{}, errors.New("上游错误退避中")
		}
		if source == "scheduler" {
			return model.SyncLog{}, nil
		}
	}
	log := model.SyncLog{Kind: kind, Source: source, StartedAt: now}
	if e = s.DB.WithContext(ctx).Create(&log).Error; e != nil {
		return log, e
	}
	impl, e := s.Registry.Get(kind, cfg.Mock, s.DevMode)
	var session *upstream.Session
	if e == nil {
		if _, stub := impl.(collector.Stub); stub {
			e = collector.ErrUnimplemented
		} else if cfg.Mock {
			session = &upstream.Session{Filters: cfg.Filters}
		} else if cfg.UpstreamAccountID == 0 {
			e = errors.New("未关联上游账号")
		} else {
			session, e = s.Upstream.Ensure(ctx, cfg.UpstreamAccountID)
			if session != nil {
				session.Filters = cfg.Filters
			}
		}
	}
	var rows []collector.RawTicket
	since := time.Time{}
	if cfg.LastSuccessAt != nil {
		since = *cfg.LastSuccessAt
	}
	if e == nil {
		rows, e = impl.List(ctx, session, since)
		if errors.Is(e, upstream.ErrSession) {
			if e = s.Upstream.Login(ctx, cfg.UpstreamAccountID, true); e == nil {
				session, e = s.Upstream.Ensure(ctx, cfg.UpstreamAccountID)
				if e == nil {
					session.Filters = cfg.Filters
					rows, e = impl.List(ctx, session, since)
				}
			}
		}
	}
	if e == nil {
		for _, raw := range rows {
			log.Count++
			detail, err := impl.Detail(ctx, session, raw.ID)
			if err != nil {
				e = err
				break
			}
			normalized, err := impl.Normalize(detail)
			if err != nil {
				e = err
				break
			}
			_, ev, err := s.Tickets.Upsert(ctx, normalized)
			if ev != nil {
				if ev.Type == "ticket.created" {
					log.Created++
				} else {
					log.Changed++
				}
			}
			if err != nil {
				e = err
				break
			}
		}
	}
	finished := s.Now()
	log.FinishedAt = &finished
	values := map[string]any{}
	next := finished.Add(Interval(cfg, finished))
	if e != nil {
		log.Error = "同步失败，请检查会话和采集器配置"
		if errors.Is(e, collector.ErrUnimplemented) {
			log.Error = collector.ErrUnimplemented.Error()
		}
		values["last_error"] = log.Error
		values["failures"] = cfg.Failures + 1
		if delay := Backoff(cfg.Failures + 1); delay > next.Sub(finished) {
			next = finished.Add(delay)
		}
	} else {
		values["last_error"] = ""
		values["failures"] = 0
		values["last_success_at"] = finished
	}
	values["next_run_at"] = next
	safe, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := s.DB.WithContext(safe).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&log).Error; err != nil {
			return err
		}
		return tx.Model(&model.Collector{}).Where("kind = ?", kind).Updates(values).Error
	}); err != nil {
		return log, err
	}
	return log, e
}
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < len(collector.Kinds); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case req := <-s.requests:
					_, _ = s.Sync(ctx, req.kind, req.source)
				}
			}
		}()
	}
	defer wg.Wait()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var rows []model.Collector
		if s.DB.WithContext(ctx).Where("enabled = ? AND (next_run_at IS NULL OR next_run_at <= ?)", true, s.Now()).Find(&rows).Error == nil {
			for _, c := range rows {
				_ = s.Trigger(ctx, c.Kind, "scheduler")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func Validate(cfg model.Collector, dev bool) error {
	if !collector.Known(cfg.Kind) {
		return errors.New("采集器类型未知")
	}
	if cfg.Mock && !dev {
		return errors.New("模拟采集仅限开发模式")
	}
	for _, seconds := range []int{cfg.IntervalSeconds, cfg.WorkIntervalSeconds, cfg.OffIntervalSeconds} {
		if seconds < 10 || seconds > 600 {
			return errors.New("采集间隔必须为 10~600 秒")
		}
	}
	if cfg.WorkStart != "" || cfg.WorkEnd != "" {
		for _, v := range []string{cfg.WorkStart, cfg.WorkEnd} {
			if _, e := time.Parse("15:04", v); e != nil || len(v) != 5 {
				return errors.New("工作时间必须为 HH:MM")
			}
		}
	}
	if cfg.Timezone != "" {
		if _, e := time.LoadLocation(cfg.Timezone); e != nil {
			return fmt.Errorf("时区不正确")
		}
	}
	return nil
}
