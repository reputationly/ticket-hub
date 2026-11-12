package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Item struct {
	JobID   uint
	Receipt string
}
type Queue interface {
	Push(context.Context, uint) error
	Pop(context.Context) (Item, error)
	Ack(context.Context, Item) error
	Close() error
}
type Memory struct {
	ch   chan Item
	done chan struct{}
	once sync.Once
}

func NewMemory() *Memory { return &Memory{ch: make(chan Item, 256), done: make(chan struct{})} }
func (q *Memory) Push(ctx context.Context, id uint) error {
	select {
	case q.ch <- Item{JobID: id}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-q.done:
		return errors.New("队列已关闭")
	}
}
func (q *Memory) Pop(ctx context.Context) (Item, error) {
	select {
	case item := <-q.ch:
		return item, nil
	case <-ctx.Done():
		return Item{}, ctx.Err()
	case <-q.done:
		return Item{}, errors.New("队列已关闭")
	}
}
func (q *Memory) Ack(context.Context, Item) error { return nil }
func (q *Memory) Close() error                    { q.once.Do(func() { close(q.done) }); return nil }

type Redis struct {
	client                  *redis.Client
	stream, group, consumer string
	reclaimAt               time.Time
}

func NewRedis(ctx context.Context, url, stream string) (*Redis, error) {
	opts, e := redis.ParseURL(url)
	if e != nil {
		return nil, e
	}
	b := make([]byte, 16)
	if _, e = rand.Read(b); e != nil {
		return nil, e
	}
	q := &Redis{client: redis.NewClient(opts), stream: stream, group: "notify-workers", consumer: hex.EncodeToString(b)}
	if e = q.client.XGroupCreateMkStream(ctx, q.stream, q.group, "0").Err(); e != nil && !strings.Contains(e.Error(), "BUSYGROUP") {
		_ = q.Close()
		return nil, e
	}
	return q, nil
}
func (q *Redis) Push(ctx context.Context, id uint) error {
	return q.client.XAdd(ctx, &redis.XAddArgs{Stream: q.stream, Values: map[string]any{"job_id": id}}).Err()
}
func (q *Redis) Pop(ctx context.Context) (Item, error) {
	for {
		var messages []redis.XMessage
		if time.Now().After(q.reclaimAt) {
			claimed, _, e := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: q.stream, Group: q.group, Consumer: q.consumer, MinIdle: time.Minute, Start: "0-0", Count: 1}).Result()
			if e != nil && !errors.Is(e, redis.Nil) {
				return Item{}, e
			}
			messages = claimed
			q.reclaimAt = time.Now().Add(time.Second * 10)
		}
		if len(messages) == 0 {
			rows, e := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: q.group, Consumer: q.consumer, Streams: []string{q.stream, ">"}, Count: 1, Block: time.Second}).Result()
			if errors.Is(e, redis.Nil) {
				if ctx.Err() != nil {
					return Item{}, ctx.Err()
				}
				continue
			}
			if e != nil {
				return Item{}, e
			}
			if len(rows) > 0 {
				messages = rows[0].Messages
			}
		}
		if len(messages) == 0 {
			continue
		}
		msg := messages[0]
		id, e := strconv.ParseUint(fmt.Sprint(msg.Values["job_id"]), 10, 64)
		item := Item{JobID: uint(id), Receipt: msg.ID}
		if e != nil || id == 0 {
			if e = q.Ack(ctx, item); e != nil {
				return Item{}, e
			}
			continue
		}
		return item, nil
	}
}
func (q *Redis) Ack(ctx context.Context, item Item) error {
	if e := q.client.XAck(ctx, q.stream, q.group, item.Receipt).Err(); e != nil {
		return e
	}
	return q.client.XDel(ctx, q.stream, item.Receipt).Err()
}
func (q *Redis) Close() error { return q.client.Close() }
