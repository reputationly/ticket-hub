package model

import (
	"encoding/json"
	"time"
)

type Ticket struct {
	ID                uint            `json:"id" gorm:"primaryKey"`
	Kind              string          `json:"kind" gorm:"uniqueIndex:ticket_upstream;size:64;not null" binding:"required,max=64"`
	UpstreamID        string          `json:"upstream_id" gorm:"uniqueIndex:ticket_upstream;size:128;not null" binding:"required,max=128"`
	Title             string          `json:"title" binding:"required,max=500"`
	Status            string          `json:"status" gorm:"index"`
	Node              string          `json:"node"`
	Site              string          `json:"site" gorm:"index"`
	Assignee          string          `json:"assignee"`
	Priority          string          `json:"priority"`
	Deadline          *time.Time      `json:"deadline"`
	CreatedAtUpstream *time.Time      `json:"created_at_upstream"`
	UpdatedAtUpstream *time.Time      `json:"updated_at_upstream"`
	Raw               json.RawMessage `json:"raw" gorm:"serializer:json;type:text"`
	Hash              string          `json:"-"`
	CreatedAt         time.Time       `json:"created_at" gorm:"index"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type TicketEvent struct {
	ID             uint            `json:"id" gorm:"primaryKey"`
	TicketID       uint            `json:"ticket_id" gorm:"index"`
	Type           string          `json:"type"`
	Before         json.RawMessage `json:"before" gorm:"serializer:json;type:text"`
	After          json.RawMessage `json:"after" gorm:"serializer:json;type:text"`
	Dispatched     bool            `json:"-" gorm:"index"`
	NextDispatchAt *time.Time      `json:"-"`
	CreatedAt      time.Time       `json:"created_at"`
}

type Channel struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	Name      string         `json:"name" binding:"required,max=100"`
	Type      string         `json:"type" binding:"required,oneof=sms_aliyun email wecom_bot feishu_bot inapp" gorm:"index"`
	Config    map[string]any `json:"config" gorm:"serializer:json;type:text"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type Conditions struct {
	Kinds      []string `json:"kinds"`
	Statuses   []string `json:"statuses"`
	Sites      []string `json:"sites"`
	Keyword    string   `json:"keyword"`
	EventTypes []string `json:"event_types"`
}
type RuleActions struct {
	UserIDs          []uint   `json:"user_ids"`
	GroupChannelIDs  []uint   `json:"group_channel_ids"`
	PersonalChannels []string `json:"personal_channels"`
	Template         string   `json:"template"`
}
type NotifyRule struct {
	ID         uint        `json:"id" gorm:"primaryKey"`
	Name       string      `json:"name" binding:"required,max=100"`
	Enabled    bool        `json:"enabled"`
	Conditions Conditions  `json:"conditions" gorm:"serializer:json;type:text"`
	Actions    RuleActions `json:"actions" gorm:"serializer:json;type:text"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}
type Subscription struct {
	UserID         uint      `json:"user_id" gorm:"primaryKey"`
	Enabled        bool      `json:"enabled"`
	Kinds          []string  `json:"kinds" gorm:"serializer:json;type:text"`
	Sites          []string  `json:"sites" gorm:"serializer:json;type:text"`
	EventTypes     []string  `json:"event_types" gorm:"serializer:json;type:text"`
	Channels       []string  `json:"channels" gorm:"serializer:json;type:text"`
	QuietStart     string    `json:"quiet_start"`
	QuietEnd       string    `json:"quiet_end"`
	Timezone       string    `json:"timezone"`
	UrgentBypass   bool      `json:"urgent_bypass"`
	MentionInGroup bool      `json:"mention_in_group"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type Notification struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	JobID     *uint     `json:"-" gorm:"uniqueIndex"`
	UserID    uint      `json:"user_id" gorm:"index"`
	Title     string    `json:"title"`
	Content   string    `json:"content" gorm:"type:text"`
	TicketID  uint      `json:"ticket_id"`
	Read      bool      `json:"read" gorm:"index"`
	CreatedAt time.Time `json:"created_at"`
}
type Delivery struct {
	UserID         uint              `json:"user_id"`
	TicketID       uint              `json:"ticket_id"`
	Title          string            `json:"title"`
	Content        string            `json:"content"`
	Phone          string            `json:"phone"`
	Email          string            `json:"email"`
	OpenID         string            `json:"open_id"`
	MentionMobiles []string          `json:"mention_mobiles"`
	MentionOpenIDs []string          `json:"mention_open_ids"`
	MentionNames   []string          `json:"mention_names"`
	Variables      map[string]string `json:"variables"`
}
type NotifyJob struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	DedupeKey     string     `json:"-" gorm:"uniqueIndex;size:128"`
	EventID       uint       `json:"event_id" gorm:"index"`
	UserID        uint       `json:"user_id" gorm:"index"`
	ChannelID     uint       `json:"channel_id" gorm:"index"`
	ChannelType   string     `json:"channel_type"`
	Delivery      Delivery   `json:"delivery" gorm:"serializer:json;type:text"`
	Status        string     `json:"status" gorm:"index"`
	Retries       int        `json:"retries"`
	Attempts      int        `json:"attempts"`
	LastError     string     `json:"last_error"`
	NextAttemptAt time.Time  `json:"next_attempt_at" gorm:"index"`
	LeaseUntil    *time.Time `json:"lease_until"`
	LeaseToken    string     `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
