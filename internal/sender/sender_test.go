package sender

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/db"
	"github.com/reputationly/ticket-hub/internal/model"
)

func TestBots(t *testing.T) {
	for _, typ := range []string{"wecom_bot", "feishu_bot"} {
		t.Run(typ, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			var payload map[string]any
			var payloads []map[string]any
			reply := `{"errcode":0,"code":0}`
			status := 200
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
					t.Error(r.Method, r.Header)
				}
				payload = nil
				if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
					t.Error(e)
				}
				payloads = append(payloads, payload)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, reply)
			}))
			defer server.Close()
			bot := &Bot{Client: server.Client(), BaseURLs: map[string]string{typ: server.URL}, Now: func() time.Time { return now }}
			ch := model.Channel{Type: typ, Config: map[string]any{"webhook": "https://not-used.invalid/secret", "secret": "local-signing-key"}}
			d := model.Delivery{Title: "新工单", Content: "请接单", MentionMobiles: []string{"13800000000"}, MentionOpenIDs: []string{"ou_test"}, MentionNames: []string{"未绑定人员"}}
			if e := bot.Send(context.Background(), ch, d, 0); e != nil {
				t.Fatal(e)
			}
			data, _ := json.Marshal(payload)
			raw := string(data)
			if typ == "wecom_bot" {
				if len(payloads) != 2 || payloads[0]["msgtype"] != "markdown" || payload["msgtype"] != "text" || !strings.Contains(raw, "13800000000") || !strings.Contains(raw, "mentioned_mobile_list") {
					t.Fatal(raw)
				}
			} else {
				mac := hmac.New(sha256.New, []byte("1700000000\nlocal-signing-key"))
				expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
				if payload["timestamp"] != "1700000000" || payload["sign"] != expected || payload["msg_type"] != "interactive" || !strings.Contains(raw, "ou_test") || !strings.Contains(raw, "未绑定人员") {
					t.Fatal(raw)
				}
			}
			for _, bad := range []string{`{"errcode":1,"code":123}`, `{}`, `not-json`} {
				reply = bad
				if e := bot.Send(context.Background(), ch, d, 0); e == nil {
					t.Fatal("错误响应未拒绝", bad)
				}
			}
			status = 500
			reply = `{}`
			if e := bot.Send(context.Background(), ch, d, 0); e == nil {
				t.Fatal("HTTP 错误未拒绝")
			}
		})
	}
}
func TestSMS(t *testing.T) {
	var values url.Values
	var auth string
	code := "OK"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		values = r.Form
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Code":"`+code+`","Message":"local","RequestId":"test"}`)
	}))
	defer server.Close()
	s := &SMS{Client: server.Client(), BaseURL: server.URL}
	ch := model.Channel{Config: map[string]any{"access_key_id": "local-id", "access_key_secret": "local-secret", "sign_name": "测试签名", "template_code": "SMS_123", "template_params": map[string]any{"name": "title", "state": "status"}}}
	d := model.Delivery{Phone: "13800000000", Variables: map[string]string{"title": "工单", "status": "待领"}}
	if e := s.Send(context.Background(), ch, d, 0); e != nil {
		t.Fatal(e)
	}
	if values.Get("PhoneNumbers") != d.Phone || values.Get("SignName") != "测试签名" || values.Get("TemplateCode") != "SMS_123" {
		t.Fatal(values)
	}
	var params map[string]string
	if e := json.Unmarshal([]byte(values.Get("TemplateParam")), &params); e != nil || params["name"] != "工单" || params["state"] != "待领" {
		t.Fatal(params, e)
	}
	if auth == "" && values.Get("Signature") == "" {
		t.Fatal("SDK 未签名")
	}
	code = "isv.INVALID_PARAMETERS"
	if e := s.Send(context.Background(), ch, d, 0); e == nil {
		t.Fatal("业务错误未拒绝")
	}
	d.Phone = ""
	if e := s.Send(context.Background(), ch, d, 0); e == nil {
		t.Fatal("空目标未拒绝")
	}
	d.Phone = "13800000000"
	ch.Config["template_params"] = map[string]any{"bad": "missing"}
	if e := s.Send(context.Background(), ch, d, 0); e == nil {
		t.Fatal("无效映射未拒绝")
	}
}
func TestChannelSecrets(t *testing.T) {
	cipher, _ := crypto.New(strings.Repeat("s", 32))
	ch := model.Channel{Type: "sms_aliyun", Config: map[string]any{"access_key_id": "local-id", "access_key_secret": "local-secret", "sign_name": "测试", "template_code": "SMS_1", "template_params": map[string]any{"name": "title"}}}
	saved, e := Prepare(cipher, ch, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(saved)
	if strings.Contains(string(raw), "local-secret") || strings.Contains(string(raw), "local-id") {
		t.Fatal("密钥明文落库")
	}
	masked := Masked(saved)
	if Text(masked.Config, "access_key_secret") != Mask {
		t.Fatal(masked)
	}
	updated, e := Prepare(cipher, masked, &saved)
	if e != nil {
		t.Fatal(e)
	}
	plain, e := Decrypt(cipher, updated)
	if e != nil || Text(plain.Config, "access_key_secret") != "local-secret" {
		t.Fatal(plain, e)
	}
	empty := model.Channel{Type: "inapp", Config: map[string]any{}}
	if _, e = Prepare(cipher, empty, nil); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []model.Channel{{Type: "unknown"}, {Type: "sms_aliyun", Config: map[string]any{"access_key_secret": Mask}}, {Type: "inapp", Config: map[string]any{"arbitrary_secret": "no"}}, {Type: "wecom_bot", Config: map[string]any{"webhook": "http://invalid"}}} {
		if _, e = Prepare(cipher, bad, nil); e == nil {
			t.Fatal("无效配置未拒绝", bad)
		}
	}
	saved.Config["access_key_secret"] = "corrupted"
	if _, e = Decrypt(cipher, saved); e == nil {
		t.Fatal("损坏密钥未拒绝")
	}
}
func TestInapp(t *testing.T) {
	database, e := db.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := database.DB()
	defer sql.Close()
	store := cache.NewMemory()
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sub, e := store.Subscribe(ctx, Topic(1))
	if e != nil {
		t.Fatal(e)
	}
	defer sub.Close()
	s := &Inapp{database, store}
	d := model.Delivery{UserID: 1, Title: "工单", Content: "提醒", TicketID: 9}
	for i := 0; i < 2; i++ {
		if e = s.Send(ctx, model.Channel{}, d, 33); e != nil {
			t.Fatal(e)
		}
	}
	var count int64
	database.Model(&model.Notification{}).Count(&count)
	if count != 1 {
		t.Fatal(count)
	}
	select {
	case raw := <-sub.Messages():
		var n model.Notification
		if e = json.Unmarshal([]byte(raw), &n); e != nil || n.UserID != 1 || n.TicketID != 9 {
			t.Fatal(raw, e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e = s.Send(ctx, model.Channel{}, model.Delivery{}, 0); e == nil {
		t.Fatal("空用户未拒绝")
	}
	_ = store.Close()
	if e = s.Send(ctx, model.Channel{}, d, 34); e == nil {
		t.Fatal("广播错误未传播")
	}
}
