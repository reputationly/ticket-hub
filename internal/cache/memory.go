package cache

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

type entry struct {
	value   string
	expires time.Time
}
type Memory struct {
	mu     sync.Mutex
	items  map[string]entry
	subs   map[string]map[*memorySub]bool
	done   chan struct{}
	closed bool
}
type memorySub struct {
	owner *Memory
	topic string
	ch    chan string
	done  chan struct{}
	once  sync.Once
}

func NewMemory() *Memory {
	m := &Memory{items: map[string]entry{}, subs: map[string]map[*memorySub]bool{}, done: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-m.done:
				return
			case <-ticker.C:
				m.mu.Lock()
				for k, e := range m.items {
					if !e.expires.After(time.Now()) {
						delete(m.items, k)
					}
				}
				m.mu.Unlock()
			}
		}
	}()
	return m
}
func (m *Memory) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.closed {
		return errors.New("缓存已关闭")
	}
	return nil
}
func (m *Memory) get(key string) (string, bool) {
	e, ok := m.items[key]
	if ok && !e.expires.After(time.Now()) {
		delete(m.items, key)
		return "", false
	}
	return e.value, ok
}
func (m *Memory) Get(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return "", err
	}
	if v, ok := m.get(key); ok {
		return v, nil
	}
	return "", ErrMiss
}
func (m *Memory) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return err
	}
	if ttl <= 0 {
		return ErrTTL
	}
	m.items[key] = entry{value, time.Now().Add(ttl)}
	return nil
}
func (m *Memory) Del(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return err
	}
	delete(m.items, key)
	return nil
}
func (m *Memory) Lock(ctx context.Context, key string, ttl time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return "", err
	}
	if ttl <= 0 {
		return "", ErrTTL
	}
	key = "lock:" + key
	if _, ok := m.get(key); ok {
		return "", ErrLock
	}
	token, err := lockToken()
	if err != nil {
		return "", err
	}
	m.items[key] = entry{token, time.Now().Add(ttl)}
	return token, nil
}
func (m *Memory) Unlock(ctx context.Context, key, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return err
	}
	key = "lock:" + key
	if v, ok := m.get(key); !ok || v != token {
		return ErrLock
	}
	delete(m.items, key)
	return nil
}
func (m *Memory) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return 0, err
	}
	if ttl <= 0 {
		return 0, ErrTTL
	}
	v, ok := m.get(key)
	n := int64(0)
	if ok {
		var err error
		n, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	n++
	expires := time.Now().Add(ttl)
	if ok {
		expires = m.items[key].expires
	}
	m.items[key] = entry{strconv.FormatInt(n, 10), expires}
	return n, nil
}
func (m *Memory) Publish(ctx context.Context, topic, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return err
	}
	for s := range m.subs[topic] {
		select {
		case s.ch <- msg:
		default:
		}
	}
	return nil
}
func (m *Memory) Subscribe(ctx context.Context, topic string) (Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return nil, err
	}
	s := &memorySub{owner: m, topic: topic, ch: make(chan string, 64), done: make(chan struct{})}
	if m.subs[topic] == nil {
		m.subs[topic] = map[*memorySub]bool{}
	}
	m.subs[topic][s] = true
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.done:
		}
	}()
	return s, nil
}
func (s *memorySub) Messages() <-chan string { return s.ch }
func (s *memorySub) Close() error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	s.close()
	return nil
}
func (s *memorySub) close() {
	s.once.Do(func() {
		delete(s.owner.subs[s.topic], s)
		if len(s.owner.subs[s.topic]) == 0 {
			delete(s.owner.subs, s.topic)
		}
		close(s.done)
		close(s.ch)
	})
}
func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.done)
		for _, subs := range m.subs {
			for s := range subs {
				s.close()
			}
		}
		m.items = map[string]entry{}
	}
	return nil
}
