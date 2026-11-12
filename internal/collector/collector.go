package collector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/upstream"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrUnimplemented = errors.New("未实现，待抓包")

type RawTicket struct {
	ID   string
	Data json.RawMessage
}
type Collector interface {
	Kind() string
	List(context.Context, *upstream.Session, time.Time) ([]RawTicket, error)
	Detail(context.Context, *upstream.Session, string) (RawTicket, error)
	Normalize(RawTicket) (model.Ticket, error)
}

var Kinds = []struct{ Kind, Name string }{{"fault_repair", "故障维修"}, {"server_offline", "服务器下架"}, {"server_reconfig", "服务器改配"}, {"logistics", "物流"}, {"inspection", "机房/服务器巡检"}, {"rack_operation", "机柜操作"}, {"assist", "协查"}, {"room_access", "入室"}, {"incident_review", "事故复盘"}, {"rack_power", "机柜上下电"}, {"security", "安全工单"}, {"event", "事件"}, {"maintenance", "维护"}, {"change", "变更"}}

func Known(kind string) bool {
	for _, k := range Kinds {
		if k.Kind == kind {
			return true
		}
	}
	return false
}

type Registry struct {
	mu          sync.RWMutex
	real, mocks map[string]Collector
}

func NewRegistry() *Registry {
	r := &Registry{real: map[string]Collector{}, mocks: map[string]Collector{}}
	for _, k := range Kinds {
		r.real[k.Kind] = Stub{k.Kind}
		r.mocks[k.Kind] = &Mock{kind: k.Kind, name: k.Name}
	}
	return r
}
func (r *Registry) Register(c Collector) { r.mu.Lock(); defer r.mu.Unlock(); r.real[c.Kind()] = c }
func (r *Registry) Get(kind string, mock, dev bool) (Collector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if mock {
		if !dev {
			return nil, errors.New("模拟采集仅限开发模式")
		}
		if c := r.mocks[kind]; c != nil {
			return c, nil
		}
	} else if c := r.real[kind]; c != nil {
		return c, nil
	}
	return nil, errors.New("采集器未注册")
}
func Seed(d *gorm.DB) error {
	for _, k := range Kinds {
		if e := d.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.Collector{Kind: k.Kind, Name: k.Name, IntervalSeconds: 30, WorkIntervalSeconds: 30, OffIntervalSeconds: 60, WorkStart: "09:00", WorkEnd: "18:00", Timezone: "Asia/Shanghai", LastError: ErrUnimplemented.Error(), Filters: map[string]any{}}).Error; e != nil {
			return e
		}
	}
	return nil
}

type Stub struct{ Type string }

func (s Stub) Kind() string { return s.Type }
func (Stub) List(context.Context, *upstream.Session, time.Time) ([]RawTicket, error) {
	return nil, ErrUnimplemented
}
func (Stub) Detail(context.Context, *upstream.Session, string) (RawTicket, error) {
	return RawTicket{}, ErrUnimplemented
}
func (Stub) Normalize(RawTicket) (model.Ticket, error) { return model.Ticket{}, ErrUnimplemented }

type Mock struct {
	mu         sync.Mutex
	kind, name string
	current    model.Ticket
	phase      int
}

func (m *Mock) Kind() string { return m.kind }
func (m *Mock) List(ctx context.Context, s *upstream.Session, since time.Time) ([]RawTicket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	var b [8]byte
	if _, e := rand.Read(b[:]); e != nil {
		return nil, e
	}
	statuses := []string{"待认领", "处理中", "已完成"}
	if m.current.ID == 0 && m.current.UpstreamID == "" || m.phase == 2 || b[0]%3 == 0 {
		m.phase = 0
		now := time.Now()
		m.current = model.Ticket{Kind: m.kind, UpstreamID: "MOCK-" + hex.EncodeToString(b[:]), Title: "模拟" + m.name + "工单", Status: statuses[0], Site: "模拟机房", Priority: "normal", CreatedAtUpstream: &now}
	} else {
		m.phase++
		m.current.Status = statuses[m.phase]
	}
	if s != nil {
		if site, ok := s.Filters["site"].(string); ok && site != "" {
			m.current.Site = site
		}
	}
	now := time.Now()
	m.current.UpdatedAtUpstream = &now
	data, e := json.Marshal(m.current)
	return []RawTicket{{ID: m.current.UpstreamID, Data: data}}, e
}
func (m *Mock) Detail(ctx context.Context, s *upstream.Session, id string) (RawTicket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current.UpstreamID != id {
		return RawTicket{}, errors.New("模拟工单不存在")
	}
	data, e := json.Marshal(m.current)
	return RawTicket{ID: id, Data: data}, e
}
func (m *Mock) Normalize(raw RawTicket) (model.Ticket, error) {
	var t model.Ticket
	if e := json.Unmarshal(raw.Data, &t); e != nil {
		return t, e
	}
	t.Kind = m.kind
	t.UpstreamID = raw.ID
	t.Raw = raw.Data
	return t, nil
}
