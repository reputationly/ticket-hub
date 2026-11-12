package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct{ client *redis.Client }

func NewRedis(ctx context.Context, url string) (*Redis, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	r := &Redis{redis.NewClient(opts)}
	if err = r.client.Ping(ctx).Err(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
func (r *Redis) Get(ctx context.Context, key string) (string, error) {
	v, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrMiss
	}
	return v, err
}
func (r *Redis) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl <= 0 {
		return ErrTTL
	}
	return r.client.Set(ctx, key, value, ttl).Err()
}
func (r *Redis) Del(ctx context.Context, key string) error { return r.client.Del(ctx, key).Err() }
func (r *Redis) Lock(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrTTL
	}
	token, err := lockToken()
	if err != nil {
		return "", err
	}
	ok, err := r.client.SetNX(ctx, "lock:"+key, token, ttl).Result()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrLock
	}
	return token, nil
}
func (r *Redis) Unlock(ctx context.Context, key, token string) error {
	n, err := r.client.Eval(ctx, `if redis.call('GET',KEYS[1]) == ARGV[1] then return redis.call('DEL',KEYS[1]) else return 0 end`, []string{"lock:" + key}, token).Int()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLock
	}
	return nil
}
func (r *Redis) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, ErrTTL
	}
	return r.client.Eval(ctx, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end; return n`, []string{key}, max(ttl.Milliseconds(), 1)).Int64()
}
func (r *Redis) Publish(ctx context.Context, topic, msg string) error {
	return r.client.Publish(ctx, topic, msg).Err()
}

type redisSub struct {
	pub    *redis.PubSub
	ch     chan string
	cancel context.CancelFunc
	once   sync.Once
	err    error
}

func (r *Redis) Subscribe(ctx context.Context, topic string) (Subscription, error) {
	ctx, cancel := context.WithCancel(ctx)
	pub := r.client.Subscribe(ctx, topic)
	if _, err := pub.Receive(ctx); err != nil {
		cancel()
		_ = pub.Close()
		return nil, err
	}
	s := &redisSub{pub: pub, ch: make(chan string, 64), cancel: cancel}
	go func() {
		defer close(s.ch)
		defer s.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-pub.Channel():
				if !ok {
					return
				}
				select {
				case s.ch <- msg.Payload:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return s, nil
}
func (s *redisSub) Messages() <-chan string { return s.ch }
func (s *redisSub) Close() error {
	s.once.Do(func() { s.cancel(); s.err = s.pub.Close() })
	return s.err
}
func (r *Redis) Close() error { return r.client.Close() }
