package db

import (
	"errors"
	"fmt"

	"github.com/glebarez/sqlite"
	"github.com/reputationly/ticket-hub/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(kind, dsn string) (*gorm.DB, error) {
	var driver gorm.Dialector
	switch kind {
	case "sqlite":
		driver = sqlite.Open(dsn)
	case "postgres":
		driver = postgres.Open(dsn)
	default:
		return nil, errors.New("不支持的数据库类型")
	}
	d, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: true})
	if err != nil {
		return nil, err
	}
	sql, err := d.DB()
	if err != nil {
		return nil, err
	}
	if kind == "sqlite" {
		sql.SetMaxOpenConns(1)
	}
	if err = d.AutoMigrate(&model.User{}, &model.Setting{}, &model.AuditLog{}, &model.Ticket{}, &model.TicketEvent{}, &model.Channel{}, &model.NotifyRule{}, &model.Subscription{}, &model.Notification{}, &model.NotifyJob{}, &model.MailAccount{}, &model.UpstreamAccount{}, &model.MailRule{}, &model.Collector{}, &model.SyncLog{}); err != nil {
		_ = sql.Close()
		return nil, err
	}
	return d, nil
}

func Bootstrap(d *gorm.DB, username, password string) error {
	var count int64
	if err := d.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if username == "" || len(username) > 64 || len(password) < 12 || len(password) > 72 {
		return fmt.Errorf("首次启动需设置 ADMIN_USERNAME 和 12~72 字节的 ADMIN_PASSWORD")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return d.Create(&model.User{Username: username, PasswordHash: string(hash), DisplayName: "超级管理员", Role: "admin", Enabled: true}).Error
}
