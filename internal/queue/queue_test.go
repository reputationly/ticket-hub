package queue

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func testQueue(t *testing.T, q Queue) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := q.Push(ctx, 42); e != nil {
		t.Fatal(e)
	}
	item, e := q.Pop(ctx)
	if e != nil || item.JobID != 42 {
		t.Fatal(item, e)
	}
	if e = q.Ack(ctx, item); e != nil {
		t.Fatal(e)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = q.Pop(stopped); e == nil {
		t.Fatal("取消后仍阻塞")
	}
}
func TestMemoryQueue(t *testing.T) { q := NewMemory(); defer q.Close(); testQueue(t, q) }
func TestRedisStreamQueue(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("未设置 TEST_REDIS_URL")
	}
	ctx := context.Background()
	stream := fmt.Sprintf("test:notify:%d", time.Now().UnixNano())
	q, e := NewRedis(ctx, url, stream)
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	defer q.client.Del(ctx, stream)
	testQueue(t, q)
	other, e := NewRedis(ctx, url, stream)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if e = q.Push(ctx, 55); e != nil {
		t.Fatal(e)
	}
	item, e := other.Pop(ctx)
	if e != nil || item.JobID != 55 {
		t.Fatal(item, e)
	}
	pending, e := q.client.XPending(ctx, stream, q.group).Result()
	if e != nil || pending.Count != 1 {
		t.Fatal(pending, e)
	}
	if e = other.Ack(ctx, item); e != nil {
		t.Fatal(e)
	}
	if e = q.Push(ctx, 77); e != nil {
		t.Fatal(e)
	}
	stale, e := q.Pop(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = q.client.Do(ctx, "XCLAIM", stream, q.group, q.consumer, 0, stale.Receipt, "IDLE", 120000).Err(); e != nil {
		t.Fatal(e)
	}
	other.reclaimAt = time.Time{}
	reclaimed, e := other.Pop(ctx)
	if e != nil || reclaimed.JobID != 77 {
		t.Fatal("未接管超时消息", reclaimed, e)
	}
	if e = other.Ack(ctx, reclaimed); e != nil {
		t.Fatal(e)
	}
	pending, e = q.client.XPending(ctx, stream, q.group).Result()
	if e != nil || pending.Count != 0 {
		t.Fatal(pending, e)
	}
	n, e := q.client.XLen(ctx, stream).Result()
	if e != nil || n != 0 {
		t.Fatal(n, e)
	}
	t.Log("真实 Redis Stream：消费组跨实例消费、ACK 与清理验证完成")
}
