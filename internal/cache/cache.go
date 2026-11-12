package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var ErrMiss = errors.New("缓存不存在")
var ErrLock = errors.New("锁已被占用或所有权不匹配")
var ErrTTL = errors.New("有效期必须大于零")

type Subscription interface {
	Messages() <-chan string
	Close() error
}

type Cache interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
	Del(context.Context, string) error
	Lock(context.Context, string, time.Duration) (string, error)
	Unlock(context.Context, string, string) error
	Incr(context.Context, string, time.Duration) (int64, error)
	Publish(context.Context, string, string) error
	Subscribe(context.Context, string) (Subscription, error)
	Close() error
}

func New(ctx context.Context, url string) (Cache, error) {
	if url == "" {
		return NewMemory(), nil
	}
	return NewRedis(ctx, url)
}

func lockToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
