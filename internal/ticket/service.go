package ticket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/model"
	"gorm.io/gorm"
)

type Dispatcher interface {
	Dispatch(context.Context, uint) error
}
type Service struct {
	DB         *gorm.DB
	Cache      cache.Cache
	Dispatcher Dispatcher
}

func Hash(t model.Ticket) string {
	data, _ := json.Marshal(struct {
		Status, Node, Assignee, Title string
		Deadline                      *time.Time
	}{t.Status, t.Node, t.Assignee, t.Title, t.Deadline})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Service) Upsert(ctx context.Context, t model.Ticket) (model.Ticket, *model.TicketEvent, error) {
	if t.Kind == "" || t.UpstreamID == "" || t.Title == "" {
		return t, nil, errors.New("工单类型、上游编号和标题不能为空")
	}
	if len(t.Raw) > 0 && !json.Valid(t.Raw) {
		return t, nil, errors.New("原始数据不是合法 JSON")
	}
	sum := sha256.Sum256([]byte(t.Kind + "\x00" + t.UpstreamID))
	key := "ticket:" + hex.EncodeToString(sum[:])
	token, err := s.Cache.Lock(ctx, key, 30*time.Second)
	if err != nil {
		return t, nil, err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Cache.Unlock(c, key, token)
	}()
	var event *model.TicketEvent
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old model.Ticket
		err := tx.Where("kind = ? AND upstream_id = ?", t.Kind, t.UpstreamID).First(&old).Error
		created := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !created {
			return err
		}
		t.Hash = Hash(t)
		t.ID = old.ID
		t.CreatedAt = old.CreatedAt
		t.UpdatedAt = time.Time{}
		if err = tx.Save(&t).Error; err != nil {
			return err
		}
		if !created && old.Hash == t.Hash {
			return nil
		}
		typ := "ticket.updated"
		if created {
			typ = "ticket.created"
		} else if old.Status != t.Status {
			typ = "ticket.status_changed"
		}
		before := json.RawMessage("null")
		if !created {
			before, _ = json.Marshal(old)
		}
		after, e := json.Marshal(t)
		if e != nil {
			return e
		}
		event = &model.TicketEvent{TicketID: t.ID, Type: typ, Before: before, After: after}
		return tx.Create(event).Error
	})
	if err != nil {
		return t, nil, err
	}
	if event != nil && s.Dispatcher != nil {
		if err = s.Dispatcher.Dispatch(ctx, event.ID); err != nil {
			return t, event, err
		}
	}
	return t, event, nil
}
