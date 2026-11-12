package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemory(t *testing.T) {
	m := NewMemory()
	defer m.Close()
	ctx := context.Background()
	if _, err := m.Get(ctx, "missing"); !errors.Is(err, ErrMiss) {
		t.Fatal(err)
	}
	if err := m.Set(ctx, "k", "value", time.Hour); err != nil {
		t.Fatal(err)
	}
	if v, err := m.Get(ctx, "k"); err != nil || v != "value" {
		t.Fatal(v, err)
	}
	if err := m.Del(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, "k"); !errors.Is(err, ErrMiss) {
		t.Fatal(err)
	}
	if err := m.Set(ctx, "bad", "", 0); !errors.Is(err, ErrTTL) {
		t.Fatal(err)
	}
	token, err := m.Lock(ctx, "work", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Lock(ctx, "work", time.Hour); !errors.Is(err, ErrLock) {
		t.Fatal(err)
	}
	if err = m.Unlock(ctx, "work", "wrong"); !errors.Is(err, ErrLock) {
		t.Fatal(err)
	}
	if err = m.Unlock(ctx, "work", token); err != nil {
		t.Fatal(err)
	}
	old, err := m.Lock(ctx, "expired", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.items["lock:expired"] = entry{old, time.Now().Add(-time.Second)}
	m.mu.Unlock()
	newToken, err := m.Lock(ctx, "expired", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Unlock(ctx, "expired", old); !errors.Is(err, ErrLock) {
		t.Fatal("旧锁删除了新锁", err)
	}
	if err = m.Unlock(ctx, "expired", newToken); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 5; i++ {
		if n, e := m.Incr(ctx, "counter", time.Hour); e != nil || n != i {
			t.Fatal(n, e)
		}
	}
	m.mu.Lock()
	deadline := m.items["counter"].expires
	m.mu.Unlock()
	_, _ = m.Incr(ctx, "counter", time.Minute)
	m.mu.Lock()
	if !m.items["counter"].expires.Equal(deadline) {
		t.Error("计数器有效期被续期")
	}
	m.items["counter"] = entry{"6", time.Now().Add(-time.Second)}
	m.mu.Unlock()
	if n, e := m.Incr(ctx, "counter", time.Hour); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	sub, err := m.Subscribe(ctx, "topic")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Publish(ctx, "topic", "你好"); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-sub.Messages():
		if msg != "你好" {
			t.Fatal(msg)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到消息")
	}
	_ = sub.Close()
	if _, ok := <-sub.Messages(); ok {
		t.Fatal("订阅未关闭")
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = m.Get(c, "x"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_ = m.Close()
	if err = m.Set(ctx, "k", "v", time.Hour); err == nil {
		t.Fatal("关闭后仍可写入")
	}
}
