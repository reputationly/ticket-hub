package model

import "time"

type User struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Username     string    `json:"username" gorm:"uniqueIndex;size:64;not null"`
	PasswordHash string    `json:"-" gorm:"not null"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role" gorm:"size:16;not null"`
	Phone        string    `json:"phone"`
	Email        string    `json:"email"`
	Enabled      bool      `json:"enabled"`
	AuthVersion  uint      `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Setting struct {
	Key       string    `json:"key" gorm:"primaryKey;size:64"`
	Value     string    `json:"value" gorm:"type:text;not null"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AuditLog struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"index"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`
}
