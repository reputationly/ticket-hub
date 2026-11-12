package sender

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/model"
)

const Mask = "********"

var secretKeys = map[string]bool{"access_key_id": true, "access_key_secret": true, "password": true, "webhook": true, "secret": true}
var fields = map[string][]string{
	"sms_aliyun": {"access_key_id", "access_key_secret", "sign_name", "template_code", "template_params"},
	"email":      {"host", "port", "username", "password", "from", "tls", "html_template"},
	"wecom_bot":  {"webhook"}, "feishu_bot": {"webhook", "secret"}, "inapp": {},
}

func Text(cfg map[string]any, key string) string { v, _ := cfg[key].(string); return v }
func SecretFields() []string {
	return []string{"access_key_id", "access_key_secret", "password", "webhook", "secret"}
}
func Masked(ch model.Channel) model.Channel {
	cfg := make(map[string]any, len(ch.Config))
	for k, v := range ch.Config {
		if secretKeys[k] && v != "" {
			cfg[k] = Mask
		} else {
			cfg[k] = v
		}
	}
	ch.Config = cfg
	return ch
}
func Decrypt(cipher *crypto.Cipher, ch model.Channel) (model.Channel, error) {
	cfg := make(map[string]any, len(ch.Config))
	for k, v := range ch.Config {
		if secretKeys[k] && v != "" {
			s, ok := v.(string)
			if !ok {
				return ch, errors.New("密钥配置错误")
			}
			plain, e := cipher.Decrypt(s)
			if e != nil {
				return ch, errors.New("渠道密钥解密失败")
			}
			cfg[k] = plain
		} else {
			cfg[k] = v
		}
	}
	ch.Config = cfg
	return ch, nil
}
func Prepare(cipher *crypto.Cipher, ch model.Channel, old *model.Channel) (model.Channel, error) {
	allowed, ok := fields[ch.Type]
	if !ok {
		return ch, errors.New("不支持的渠道类型")
	}
	if old != nil && ch.Type != old.Type {
		return ch, errors.New("渠道类型不可修改")
	}
	cfg := make(map[string]any, len(ch.Config))
	for k, v := range ch.Config {
		found := false
		for _, key := range allowed {
			if key == k {
				found = true
			}
		}
		if !found {
			return ch, fmt.Errorf("未知渠道配置字段：%s", k)
		}
		cfg[k] = v
	}
	ch.Config = cfg
	for _, key := range allowed {
		if key == "template_params" {
			if v, ok := cfg[key]; ok {
				m, ok := v.(map[string]any)
				if !ok {
					return ch, errors.New("模板参数映射格式错误")
				}
				for _, v := range m {
					if _, ok := v.(string); !ok {
						return ch, errors.New("模板参数必须映射到模板字段名")
					}
				}
			}
			continue
		}
		if v, ok := cfg[key]; ok {
			if _, ok := v.(string); !ok {
				return ch, fmt.Errorf("%s 必须是字符串", key)
			}
		}
	}
	for key := range secretKeys {
		if v, ok := cfg[key]; ok && v == Mask {
			if old == nil || Text(old.Config, key) == "" {
				return ch, errors.New("新密钥不能使用打码值")
			}
			plain, e := cipher.Decrypt(Text(old.Config, key))
			if e != nil {
				return ch, errors.New("原密钥解密失败")
			}
			cfg[key] = plain
		} else if old != nil && !ok {
			if encrypted := Text(old.Config, key); encrypted != "" {
				plain, e := cipher.Decrypt(encrypted)
				if e != nil {
					return ch, e
				}
				cfg[key] = plain
			}
		}
	}
	required := map[string][]string{"sms_aliyun": {"access_key_id", "access_key_secret", "sign_name", "template_code"}, "email": {"host", "port", "from", "tls"}, "wecom_bot": {"webhook"}, "feishu_bot": {"webhook"}}
	for _, key := range required[ch.Type] {
		if Text(cfg, key) == "" {
			return ch, fmt.Errorf("缺少渠道配置：%s", key)
		}
	}
	if ch.Type == "wecom_bot" || ch.Type == "feishu_bot" {
		u, e := url.Parse(Text(cfg, "webhook"))
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return ch, errors.New("Webhook 必须为 HTTPS 地址")
		}
	}
	if ch.Type == "email" {
		port, e := strconv.Atoi(Text(cfg, "port"))
		if e != nil || port < 1 || port > 65535 {
			return ch, errors.New("SMTP 端口不正确")
		}
		if strings.ContainsAny(Text(cfg, "host"), "\r\n/ ") {
			return ch, errors.New("SMTP 主机不正确")
		}
		if _, e = mail.ParseAddress(Text(cfg, "from")); e != nil {
			return ch, errors.New("发件人格式错误")
		}
		switch Text(cfg, "tls") {
		case "ssl", "starttls", "none":
		default:
			return ch, errors.New("SMTP TLS 模式错误")
		}
	}
	for key := range secretKeys {
		if plain := Text(cfg, key); plain != "" {
			encrypted, e := cipher.Encrypt(plain)
			if e != nil {
				return ch, e
			}
			cfg[key] = encrypted
		}
	}
	return ch, nil
}
