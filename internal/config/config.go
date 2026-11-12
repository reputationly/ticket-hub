package config

import (
	"errors"
	"os"
)

type Config struct {
	Address, DBType, DBDSN, RedisURL, Secret, AdminUsername, AdminPassword string
	CookieSecure                                                           bool
	DevMode                                                                bool
}

func Load() (Config, error) {
	c := Config{Address: value("LISTEN_ADDR", ":8080"), DBType: value("DB_TYPE", "sqlite"), DBDSN: value("DB_DSN", "./data.db"), RedisURL: os.Getenv("REDIS_URL"), Secret: os.Getenv("APP_SECRET_KEY"), AdminUsername: os.Getenv("ADMIN_USERNAME"), AdminPassword: os.Getenv("ADMIN_PASSWORD"), CookieSecure: os.Getenv("COOKIE_SECURE") == "true"}
	c.DevMode = os.Getenv("DEV_MODE") == "true"
	if len(c.Secret) < 32 {
		return c, errors.New("APP_SECRET_KEY 至少需要 32 字节")
	}
	if c.DBType != "sqlite" && c.DBType != "postgres" {
		return c, errors.New("DB_TYPE 仅支持 sqlite 或 postgres")
	}
	return c, nil
}

func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
