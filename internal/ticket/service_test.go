package ticket

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/db"
	"github.com/reputationly/ticket-hub/internal/model"
	"gorm.io/gorm"
)

type dispatchFunc func(context.Context, uint) error

func (f dispatchFunc) Dispatch(ctx context.Context, id uint) error { return f(ctx, id) }
func testUpsert(t *testing.T, d *gorm.DB) {
	ctx := context.Background()
	store := cache.NewMemory()
	defer store.Close()
	var ids []uint
	s := Service{DB: d, Cache: store, Dispatcher: dispatchFunc(func(ctx context.Context, id uint) error {
		var e model.TicketEvent
		if err := d.WithContext(ctx).First(&e, id).Error; err != nil {
			t.Fatal("事件必须先提交", err)
		}
		ids = append(ids, id)
		return nil
	})}
	ticket := model.Ticket{Kind: "repair", UpstreamID: "TEST", Title: "测试工单", Status: "待认领", Site: "北京"}
	got, event, e := s.Upsert(ctx, ticket)
	if e != nil || event == nil || event.Type != "ticket.created" {
		t.Fatal(got, event, e)
	}
	if len(ids) != 1 {
		t.Fatal(ids)
	}
	ticket = got
	ticket.Raw = json.RawMessage(`{"changed":true}`)
	ticket.Site = "上海"
	got, event, e = s.Upsert(ctx, ticket)
	if e != nil || event != nil || len(ids) != 1 {
		t.Fatal(got, event, e)
	}
	for _, mutate := range []func(*model.Ticket){func(t *model.Ticket) { t.Node = "接单" }, func(t *model.Ticket) { t.Assignee = "工程师" }, func(t *model.Ticket) { v := time.Now().UTC().Truncate(time.Second); t.Deadline = &v }, func(t *model.Ticket) { t.Title = "新标题" }, func(t *model.Ticket) { t.Deadline = nil }} {
		mutate(&ticket)
		got, event, e = s.Upsert(ctx, ticket)
		if e != nil || event == nil || event.Type != "ticket.updated" {
			t.Fatal(got, event, e)
		}
	}
	ticket.Status = "处理中"
	got, event, e = s.Upsert(ctx, ticket)
	if e != nil || event == nil || event.Type != "ticket.status_changed" {
		t.Fatal(got, event, e)
	}
	var before, after model.Ticket
	if e = json.Unmarshal(event.Before, &before); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(event.After, &after); e != nil || before.Status != "待认领" || after.Status != "处理中" {
		t.Fatal(before, after, e)
	}
	var count int64
	d.Model(&model.Ticket{}).Where("kind = ? AND upstream_id = ?", ticket.Kind, ticket.UpstreamID).Count(&count)
	if count != 1 {
		t.Fatal(count)
	}
	s.Dispatcher = dispatchFunc(func(context.Context, uint) error { return errors.New("队列暂不可用") })
	ticket.Title = "等待恢复"
	_, event, e = s.Upsert(ctx, ticket)
	if e == nil || event == nil {
		t.Fatal(event, e)
	}
	var persisted model.TicketEvent
	if e = d.First(&persisted, event.ID).Error; e != nil || persisted.Dispatched {
		t.Fatal(persisted, e)
	}
	if _, _, e = s.Upsert(ctx, model.Ticket{}); e == nil {
		t.Fatal("空工单未拒绝")
	}
	ticket.Raw = json.RawMessage("invalid")
	if _, _, e = s.Upsert(ctx, ticket); e == nil {
		t.Fatal("无效原始 JSON 未拒绝")
	}
	ticket.Raw = nil
	_ = store.Close()
	if _, _, e = s.Upsert(ctx, ticket); e == nil {
		t.Fatal("缓存错误未传播")
	}
	t.Log("工单唯一键、字段哈希、事件快照与提交后投递验证完成")
}
func TestUpsert(t *testing.T) {
	d, e := db.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := d.DB()
	defer sql.Close()
	testUpsert(t, d)
}
func TestPGTickets(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_PG_DSN")
	}
	d, e := db.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := d.DB()
	defer sql.Close()
	tx := d.Begin()
	defer tx.Rollback()
	testUpsert(t, tx)
}
