package model

import "time"

type MailAccount struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" binding:"required,max=100"`
	Host      string    `json:"host" binding:"required,max=254"`
	Port      int       `json:"port" binding:"min=1,max=65535"`
	TLS       string    `json:"tls" binding:"required,oneof=ssl starttls none"`
	Username  string    `json:"username" binding:"required,max=254"`
	Password  string    `json:"password"`
	SMTPHost  string    `json:"smtp_host"`
	SMTPPort  int       `json:"smtp_port"`
	SMTPTLS   string    `json:"smtp_tls"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type CodeFilter struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Code    string `json:"code"`
}
type UpstreamAccount struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	Name          string     `json:"name" binding:"required,max=100"`
	Type          string     `json:"type" binding:"required,oneof=dora mock"`
	Email         string     `json:"email" binding:"required,email"`
	MailAccountID uint       `json:"mail_account_id"`
	Filter        CodeFilter `json:"filter" gorm:"serializer:json;type:text"`
	IAMBase       string     `json:"iam_base"`
	BusinessBase  string     `json:"business_base"`
	CheckURL      string     `json:"check_url"`
	AccountType   string     `json:"account_type"`
	Enabled       bool       `json:"enabled"`
	CookieJar     string     `json:"-" gorm:"type:text"`
	Challenge     string     `json:"-" gorm:"type:text"`
	ChallengeAt   *time.Time `json:"-"`
	State         string     `json:"state"`
	LastLoginAt   *time.Time `json:"last_login_at"`
	Failures      int        `json:"failures"`
	LastError     string     `json:"last_error"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
type MailRule struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	Name          string    `json:"name" binding:"required,max=100"`
	MailAccountID uint      `json:"mail_account_id" binding:"required"`
	From          string    `json:"from"`
	Subject       string    `json:"subject"`
	Kind          string    `json:"kind" binding:"required"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
type Collector struct {
	Kind                string         `json:"kind" gorm:"primaryKey;size:64"`
	Name                string         `json:"name"`
	Enabled             bool           `json:"enabled"`
	Mock                bool           `json:"mock"`
	UpstreamAccountID   uint           `json:"upstream_account_id"`
	IntervalSeconds     int            `json:"interval_seconds"`
	WorkIntervalSeconds int            `json:"work_interval_seconds"`
	OffIntervalSeconds  int            `json:"off_interval_seconds"`
	WorkStart           string         `json:"work_start"`
	WorkEnd             string         `json:"work_end"`
	Timezone            string         `json:"timezone"`
	Filters             map[string]any `json:"filters" gorm:"serializer:json;type:text"`
	LastSuccessAt       *time.Time     `json:"last_success_at"`
	LastError           string         `json:"last_error"`
	Failures            int            `json:"failures"`
	NextRunAt           *time.Time     `json:"next_run_at" gorm:"index"`
	UpdatedAt           time.Time      `json:"updated_at"`
}
type SyncLog struct {
	ID         uint       `json:"id" gorm:"primaryKey"`
	Kind       string     `json:"kind" gorm:"index"`
	Source     string     `json:"source"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Count      int        `json:"count"`
	Created    int        `json:"created"`
	Changed    int        `json:"changed"`
	Error      string     `json:"error"`
}
