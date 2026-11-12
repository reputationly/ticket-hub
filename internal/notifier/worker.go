package notifier

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/sender"
	"gorm.io/gorm"
)

var RetryDelays = []time.Duration{time.Second, 10 * time.Second, time.Minute, 5 * time.Minute}

func (s *Service) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			item, e := s.Queue.Pop(ctx)
			if e != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Error("通知队列读取失败")
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
				continue
			}
			if e = s.Process(ctx, item.JobID); e != nil {
				slog.Error("通知任务处理失败", "job_id", item.JobID)
				continue
			}
			if e = s.Queue.Ack(ctx, item); e != nil {
				slog.Error("通知任务确认失败", "job_id", item.JobID)
			}
		}
	}()
	defer wg.Wait()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if e := s.Recover(ctx); e != nil && ctx.Err() == nil {
			slog.Error("通知任务恢复失败")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) Recover(ctx context.Context) error {
	var events []model.TicketEvent
	if e := s.DB.WithContext(ctx).Where("dispatched = ? AND (next_dispatch_at IS NULL OR next_dispatch_at <= ?)", false, s.Now()).Order("id ASC").Limit(100).Find(&events).Error; e != nil {
		return e
	}
	for _, event := range events {
		if e := s.Dispatch(ctx, event.ID); e != nil {
			slog.Error("工单事件投递失败", "event_id", event.ID)
			if err := s.DB.WithContext(ctx).Model(&model.TicketEvent{}).Where("id = ? AND dispatched = ?", event.ID, false).Update("next_dispatch_at", s.Now().Add(10*time.Second)).Error; err != nil {
				return err
			}
		}
	}
	var jobs []model.NotifyJob
	now := s.Now()
	if e := s.DB.WithContext(ctx).Where("(status = ? AND next_attempt_at <= ?) OR (status = ? AND (lease_until IS NULL OR lease_until <= ?))", "pending", now, "processing", now).Order("next_attempt_at ASC, id ASC").Limit(100).Find(&jobs).Error; e != nil {
		return e
	}
	for _, job := range jobs {
		_, e := s.Cache.Lock(ctx, fmt.Sprintf("notify-enqueue:%d", job.ID), 2*time.Second)
		if errors.Is(e, cache.ErrLock) {
			continue
		}
		if e != nil {
			return e
		}
		if e = s.Queue.Push(ctx, job.ID); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) Process(ctx context.Context, id uint) error {
	now := s.Now()
	token := make([]byte, 16)
	if _, e := rand.Read(token); e != nil {
		return e
	}
	lease := now.Add(time.Minute)
	owner := hex.EncodeToString(token)
	result := s.DB.WithContext(ctx).Model(&model.NotifyJob{}).Where("id = ? AND ((status = ? AND next_attempt_at <= ?) OR (status = ? AND (lease_until IS NULL OR lease_until <= ?)))", id, "pending", now, "processing", now).Updates(map[string]any{"status": "processing", "lease_until": lease, "lease_token": owner})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	var job model.NotifyJob
	if e := s.DB.WithContext(ctx).First(&job, id).Error; e != nil {
		return e
	}
	finish := func(values map[string]any) error {
		values["lease_until"] = nil
		values["lease_token"] = ""
		return s.DB.WithContext(ctx).Model(&model.NotifyJob{}).Where("id = ? AND lease_token = ?", id, owner).Updates(values).Error
	}
	var ch model.Channel
	if job.ChannelType == "inapp" {
		ch = model.Channel{Type: "inapp", Enabled: true}
	} else {
		if e := s.DB.WithContext(ctx).First(&ch, job.ChannelID).Error; e != nil {
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			return finish(map[string]any{"status": "failed", "last_error": "渠道已删除"})
		}
		if !ch.Enabled {
			return finish(map[string]any{"status": "failed", "last_error": "渠道已停用"})
		}
		var e error
		ch, e = sender.Decrypt(s.Cipher, ch)
		if e != nil {
			return s.retry(ctx, job, owner, e)
		}
	}
	if job.UserID != 0 {
		var u model.User
		if e := s.DB.WithContext(ctx).First(&u, job.UserID).Error; e != nil {
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			return finish(map[string]any{"status": "failed", "last_error": "用户已删除"})
		}
		if !u.Enabled {
			return finish(map[string]any{"status": "failed", "last_error": "用户已停用"})
		}
	}
	delay, e := s.Allow(ctx, ch, job.Delivery, now)
	if e != nil {
		return finish(map[string]any{"status": "pending", "last_error": "限频缓存不可用", "next_attempt_at": now.Add(time.Second)})
	}
	if delay > 0 {
		return finish(map[string]any{"status": "pending", "last_error": "限频延迟", "next_attempt_at": now.Add(delay)})
	}
	impl, ok := s.Senders[ch.Type]
	if !ok {
		return s.retry(ctx, job, owner, errors.New("渠道类型不支持"))
	}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	e = impl.Send(sendCtx, ch, job.Delivery, job.ID)
	if e != nil {
		message := e.Error()
		for _, key := range sender.SecretFields() {
			if secret := sender.Text(ch.Config, key); secret != "" {
				message = strings.ReplaceAll(message, secret, "[已隐藏]")
			}
		}
		return s.retry(ctx, job, owner, errors.New(message))
	}
	return finish(map[string]any{"status": "sent", "last_error": "", "attempts": job.Attempts + 1})
}
func (s *Service) retry(ctx context.Context, job model.NotifyJob, owner string, cause error) error {
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	values := map[string]any{"attempts": job.Attempts + 1, "last_error": message, "lease_until": nil, "lease_token": "", "status": "failed"}
	if job.Retries < len(RetryDelays) {
		values["status"] = "pending"
		values["next_attempt_at"] = s.Now().Add(RetryDelays[job.Retries])
		values["retries"] = job.Retries + 1
	}
	return s.DB.WithContext(ctx).Model(&model.NotifyJob{}).Where("id = ? AND lease_token = ?", job.ID, owner).Updates(values).Error
}
func (s *Service) Allow(ctx context.Context, ch model.Channel, d model.Delivery, now time.Time) (time.Duration, error) {
	type limit struct {
		count  int64
		window time.Duration
		key    string
	}
	var limits []limit
	hash := func(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
	switch ch.Type {
	case "wecom_bot":
		limits = []limit{{20, time.Minute, "wecom:" + hash(sender.Text(ch.Config, "webhook"))}}
	case "feishu_bot":
		key := hash(sender.Text(ch.Config, "webhook"))
		limits = []limit{{5, time.Second, "feishu-sec:" + key}, {100, time.Minute, "feishu-min:" + key}}
	case "sms_aliyun":
		limits = []limit{{1, time.Minute, "sms:" + hash(d.Phone)}}
	}
	var delay time.Duration
	weight := 1
	if ch.Type == "wecom_bot" && len(d.MentionMobiles) > 0 {
		weight = 2
	}
	for _, l := range limits {
		bucket := now.UnixNano() / int64(l.window)
		boundary := time.Unix(0, (bucket+1)*int64(l.window))
		ttl := boundary.Sub(now)
		var n int64
		for i := 0; i < weight; i++ {
			var e error
			n, e = s.Cache.Incr(ctx, fmt.Sprintf("notify-rate:%s:%d", l.key, bucket), ttl)
			if e != nil {
				return 0, e
			}
		}
		if n > l.count && ttl > delay {
			delay = ttl
		}
	}
	return delay, nil
}
func (s *Service) Resend(ctx context.Context, id uint) error {
	now := s.Now()
	result := s.DB.WithContext(ctx).Model(&model.NotifyJob{}).Where("id = ? AND status <> ?", id, "processing").Updates(map[string]any{"status": "pending", "retries": 0, "last_error": "", "next_attempt_at": now, "lease_until": nil, "lease_token": ""})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("任务正在处理或不存在")
	}
	return s.Queue.Push(ctx, id)
}
