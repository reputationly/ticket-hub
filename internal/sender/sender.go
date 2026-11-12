package sender

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Sender interface {
	Send(context.Context, model.Channel, model.Delivery, uint) error
}
type Func func(context.Context, model.Channel, model.Delivery, uint) error

func (f Func) Send(ctx context.Context, ch model.Channel, d model.Delivery, jobID uint) error {
	return f(ctx, ch, d, jobID)
}

type Options struct {
	HTTPClient    *http.Client
	BaseURLs      map[string]string
	Now           func() time.Time
	SMTPTLSConfig TLSConfig
}
type Registry map[string]Sender

func New(db *gorm.DB, store cache.Cache, opts Options) Registry {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	bot := &Bot{Client: opts.HTTPClient, BaseURLs: opts.BaseURLs, Now: opts.Now}
	return Registry{"wecom_bot": bot, "feishu_bot": bot, "email": &SMTP{TLSConfig: opts.SMTPTLSConfig}, "sms_aliyun": &SMS{Client: opts.HTTPClient, BaseURL: opts.BaseURLs["sms_aliyun"]}, "inapp": &Inapp{DB: db, Cache: store}}
}

type Bot struct {
	Client   *http.Client
	BaseURLs map[string]string
	Now      func() time.Time
}

func FeishuSign(timestamp, secret string) string {
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
func (b *Bot) Send(ctx context.Context, ch model.Channel, d model.Delivery, _ uint) error {
	endpoint := Text(ch.Config, "webhook")
	if v := b.BaseURLs[ch.Type]; v != "" {
		endpoint = v
	}
	var payload map[string]any
	if ch.Type == "wecom_bot" {
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]any{"content": d.Content}}
	} else {
		content := d.Content
		for _, id := range d.MentionOpenIDs {
			content += "\n<at id=" + strconv.Quote(id) + "></at>"
		}
		for _, name := range d.MentionNames {
			content += "\n@" + name
		}
		payload = map[string]any{"msg_type": "interactive", "card": map[string]any{"header": map[string]any{"title": map[string]string{"tag": "plain_text", "content": d.Title}}, "elements": []any{map[string]string{"tag": "markdown", "content": content}}}}
		if secret := Text(ch.Config, "secret"); secret != "" {
			ts := strconv.FormatInt(b.Now().Unix(), 10)
			payload["timestamp"] = ts
			payload["sign"] = FeishuSign(ts, secret)
		}
	}
	if e := b.post(ctx, endpoint, ch.Type, payload); e != nil {
		return e
	}
	if ch.Type == "wecom_bot" && len(d.MentionMobiles) > 0 {
		return b.post(ctx, endpoint, ch.Type, map[string]any{"msgtype": "text", "text": map[string]any{"content": "工单提醒：" + d.Title, "mentioned_mobile_list": d.MentionMobiles}})
	}
	return nil
}
func (b *Bot) post(ctx context.Context, endpoint, kind string, payload map[string]any) error {
	data, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if e != nil {
		return errors.New("机器人地址错误")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := b.Client.Do(req)
	if e != nil {
		return errors.New("机器人请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("机器人 HTTP 状态 %d", resp.StatusCode)
	}
	var result struct {
		ErrCode    *int `json:"errcode"`
		Code       *int `json:"code"`
		StatusCode *int `json:"StatusCode"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); e != nil {
		return errors.New("机器人响应格式错误")
	}
	code := result.Code
	if kind == "wecom_bot" {
		code = result.ErrCode
	} else if code == nil {
		code = result.StatusCode
	}
	if code == nil {
		return errors.New("机器人响应缺少状态码")
	}
	if *code != 0 {
		return fmt.Errorf("机器人返回错误码 %d", *code)
	}
	return nil
}

type Inapp struct {
	DB    *gorm.DB
	Cache cache.Cache
}

func Topic(userID uint) string { return fmt.Sprintf("notifications:%d", userID) }
func (s *Inapp) Send(ctx context.Context, _ model.Channel, d model.Delivery, jobID uint) error {
	if d.UserID == 0 {
		return errors.New("站内消息缺少用户")
	}
	n := model.Notification{UserID: d.UserID, Title: d.Title, Content: d.Content, TicketID: d.TicketID}
	if jobID != 0 {
		n.JobID = &jobID
	}
	result := s.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&n)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		if e := s.DB.WithContext(ctx).Where("job_id = ?", jobID).First(&n).Error; e != nil {
			return e
		}
	}
	data, e := json.Marshal(n)
	if e != nil {
		return e
	}
	return s.Cache.Publish(ctx, Topic(d.UserID), strings.TrimSpace(string(data)))
}
