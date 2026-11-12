package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestRedisCache(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("未设置 TEST_REDIS_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := NewRedis(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	key := fmt.Sprintf("test:%d", time.Now().UnixNano())
	defer r.Del(context.Background(), key)
	if _, err = r.Get(ctx, key); !errors.Is(err, ErrMiss) {
		t.Fatal(err)
	}
	if err = r.Set(ctx, key, "你好", time.Second); err != nil {
		t.Fatal(err)
	}
	if v, e := r.Get(ctx, key); e != nil || v != "你好" {
		t.Fatal(v, e)
	}
	if err = r.Del(ctx, key); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 3; i++ {
		if n, e := r.Incr(ctx, key, time.Second); e != nil || n != i {
			t.Fatal(n, e)
		}
	}
	if ttl, e := r.client.PTTL(ctx, key).Result(); e != nil || ttl <= 0 || ttl > time.Second {
		t.Fatal(ttl, e)
	}
	lock, e := r.Lock(ctx, key, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Lock(ctx, key, time.Second); !errors.Is(e, ErrLock) {
		t.Fatal(e)
	}
	if e = r.Unlock(ctx, key, "wrong"); !errors.Is(e, ErrLock) {
		t.Fatal(e)
	}
	if e = r.Unlock(ctx, key, lock); e != nil {
		t.Fatal(e)
	}
	sub, e := r.Subscribe(ctx, key)
	if e != nil {
		t.Fatal(e)
	}
	defer sub.Close()
	if e = r.Publish(ctx, key, "广播"); e != nil {
		t.Fatal(e)
	}
	select {
	case msg := <-sub.Messages():
		if msg != "广播" {
			t.Fatal(msg)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e = r.Set(ctx, key, "短期", time.Millisecond*20); e != nil {
		t.Fatal(e)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond * 10)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("缓存未过期")
		case <-tick.C:
			if _, e = r.Get(ctx, key); errors.Is(e, ErrMiss) {
				t.Log("真实 Redis：TTL、锁、计数器、Pub/Sub 验证完成")
				return
			}
		}
	}
}
