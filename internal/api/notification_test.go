package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/notifier"
	"github.com/reputationly/ticket-hub/internal/queue"
	"github.com/reputationly/ticket-hub/internal/sender"
)

func notificationFixture(t *testing.T) (*fixture, *notifier.Service) {
	t.Helper()
	f := setup(t)
	cipher, _ := crypto.New(f.cfg.Secret)
	q := queue.NewMemory()
	t.Cleanup(func() { q.Close() })
	n := notifier.New(f.db, f.store, cipher, q, sender.New(f.db, f.store, sender.Options{}))
	f.cfg.DevMode = true
	f.router = New(f.db, f.store, f.cfg, n)
	return f, n
}
func responseID(t *testing.T, w *httptest.ResponseRecorder) uint {
	t.Helper()
	var result struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.Data.ID == 0 {
		t.Fatal(w.Body.String(), e)
	}
	return result.Data.ID
}
func TestNotificationLifecycle(t *testing.T) {
	f, n := notificationFixture(t)
	admin := loginAs(t, f.router, "admin", testPassword)
	uid := create(t, f, admin, "notify_member", "user")
	member := loginAs(t, f.router, "notify_member", testPassword)
	status(t, request(t, f.router, "POST", "/api/admin/channels", `{"name":"站内","type":"inapp","enabled":true,"config":{}}`, member), 403)
	w := request(t, f.router, "POST", "/api/admin/channels", `{"name":"站内","type":"inapp","enabled":true,"config":{}}`, admin)
	status(t, w, 200)
	channelID := responseID(t, w)
	status(t, request(t, f.router, "PUT", "/api/me/profile", `{"phone":"13800000000","email":"member@example.com","feishu_open_id":"ou_member"}`, member), 200)
	status(t, request(t, f.router, "PUT", "/api/me/subscription", `{"enabled":true,"kinds":["repair"],"channels":["inapp"],"quiet_start":"22:00","quiet_end":"07:00","timezone":"Asia/Shanghai","urgent_bypass":true,"mention_in_group":true}`, member), 200)
	status(t, request(t, f.router, "GET", "/api/me/subscription", "", member), 200)
	w = request(t, f.router, "POST", "/api/admin/rules", fmt.Sprintf(`{"name":"维修提醒","enabled":true,"conditions":{"kinds":["repair"],"event_types":["ticket.created"]},"actions":{"user_ids":[%d],"personal_channels":["inapp"]}}`, uid), admin)
	status(t, w, 200)
	ruleID := responseID(t, w)
	status(t, request(t, f.router, "PUT", "/api/admin/settings", `{"site_name":"工单中心","public_url":"https://tickets.example.com","session_hours":24}`, admin), 200)
	status(t, request(t, f.router, "POST", "/api/dev/tickets", `{"kind":"repair","upstream_id":"DEV-1","title":"维修工单","status":"待领","site":"北京"}`, member), 403)
	w = request(t, f.router, "POST", "/api/dev/tickets", `{"kind":"repair","upstream_id":"DEV-1","title":"维修工单","status":"待领","site":"北京"}`, admin)
	status(t, w, 200)
	var jobs []model.NotifyJob
	if e := f.db.Find(&jobs).Error; e != nil || len(jobs) != 1 {
		t.Fatal(jobs, e)
	}
	if e := n.Process(context.Background(), jobs[0].ID); e != nil {
		t.Fatal(e)
	}
	w = request(t, f.router, "GET", "/api/notifications", "", member)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), "维修工单") || !strings.Contains(w.Body.String(), "https://tickets.example.com/tickets/") {
		t.Fatal(w.Body.String())
	}
	status(t, request(t, f.router, "GET", "/api/notifications/unread-count", "", member), 200)
	var note model.Notification
	f.db.First(&note)
	path := fmt.Sprintf("/api/notifications/%d/read", note.ID)
	status(t, request(t, f.router, "PUT", path, "", admin), 404)
	status(t, request(t, f.router, "PUT", path, "", member), 200)
	w = request(t, f.router, "GET", "/api/notifications/unread-count", "", member)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"count":0`) {
		t.Fatal(w.Body.String())
	}
	status(t, request(t, f.router, "PUT", "/api/notifications/read-all", "", member), 200)
	for _, path := range []string{"/api/tickets?kind=repair&status=待领&site=北京&keyword=维修&page_size=1", "/api/tickets/1", "/api/admin/channels", "/api/admin/channels/1", "/api/admin/rules", "/api/admin/notify-jobs?status=sent"} {
		cookie := admin
		if strings.HasPrefix(path, "/api/tickets") {
			cookie = member
		}
		status(t, request(t, f.router, "GET", path, "", cookie), 200)
	}
	status(t, request(t, f.router, "GET", "/api/tickets?from=bad", "", member), 400)
	status(t, request(t, f.router, "GET", "/api/tickets/999", "", member), 404)
	status(t, request(t, f.router, "GET", "/api/tickets/bad", "", member), 400)
	w = request(t, f.router, "GET", "/api/tickets?site=不存在", "", member)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal(w.Body.String())
	}
	status(t, request(t, f.router, "POST", "/api/admin/templates/preview", `{"template":"{{.Ticket.Title}} {{.Link}}","ticket":{"id":9,"title":"预览"}}`, admin), 200)
	status(t, request(t, f.router, "POST", "/api/admin/templates/preview", `{"template":"{{.Missing}}"}`, admin), 400)
	status(t, request(t, f.router, "POST", fmt.Sprintf("/api/admin/channels/%d/test", channelID), fmt.Sprintf(`{"user_id":%d}`, uid), admin), 200)
	status(t, request(t, f.router, "POST", fmt.Sprintf("/api/admin/notify-jobs/%d/resend", jobs[0].ID), `{}`, admin), 200)
	status(t, request(t, f.router, "PUT", fmt.Sprintf("/api/admin/rules/%d", ruleID), `{"name":"已禁用","enabled":false,"actions":{}}`, admin), 200)
	status(t, request(t, f.router, "DELETE", fmt.Sprintf("/api/admin/rules/%d", ruleID), "", admin), 200)
	status(t, request(t, f.router, "DELETE", fmt.Sprintf("/api/admin/channels/%d", channelID), "", admin), 200)
	cfg := f.cfg
	cfg.DevMode = false
	off := New(f.db, f.store, cfg)
	status(t, request(t, off, "POST", "/api/dev/tickets", `{}`, admin), 404)
}
func TestChannelMaskAPI(t *testing.T) {
	f, _ := notificationFixture(t)
	admin := loginAs(t, f.router, "admin", testPassword)
	w := request(t, f.router, "POST", "/api/admin/channels", `{"name":"企微","type":"wecom_bot","enabled":true,"config":{"webhook":"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=local-secret"}}`, admin)
	status(t, w, 200)
	id := responseID(t, w)
	if strings.Contains(w.Body.String(), "local-secret") || !strings.Contains(w.Body.String(), sender.Mask) {
		t.Fatal(w.Body.String())
	}
	var saved model.Channel
	f.db.First(&saved, id)
	if strings.Contains(sender.Text(saved.Config, "webhook"), "local-secret") {
		t.Fatal("明文保存")
	}
	path := fmt.Sprintf("/api/admin/channels/%d", id)
	status(t, request(t, f.router, "PUT", path, `{"name":"更新","type":"wecom_bot","enabled":true,"config":{"webhook":"********"}}`, admin), 200)
	f.db.First(&saved, id)
	cipher, _ := crypto.New(f.cfg.Secret)
	plain, e := sender.Decrypt(cipher, saved)
	if e != nil || !strings.Contains(sender.Text(plain.Config, "webhook"), "local-secret") {
		t.Fatal(e)
	}
	for _, tc := range []struct{ path, body string }{{"/api/admin/channels", `{"name":"无效","type":"email","config":{}}`}, {"/api/admin/channels", `{}`}, {"/api/admin/rules", `{"name":"坏规则","actions":{"personal_channels":["wecom_bot"]}}`}, {"/api/admin/rules", `{"name":"坏规则","actions":{"group_channel_ids":[999]}}`}} {
		status(t, request(t, f.router, "POST", tc.path, tc.body, admin), 400)
	}
	for _, body := range []string{`{"channels":["wecom_bot"]}`, `{"channels":["email"]}`, `{"quiet_start":"25:00","quiet_end":"06:00"}`} {
		status(t, request(t, f.router, "PUT", "/api/me/subscription", body, admin), 400)
	}
	status(t, request(t, f.router, "PUT", "/api/me/profile", `{"phone":"bad\r\n"}`, admin), 400)
	status(t, request(t, f.router, "GET", "/api/notifications?read=bad", "", admin), 400)
	status(t, request(t, f.router, "GET", "/api/admin/channels/999", "", admin), 404)
	status(t, request(t, f.router, "POST", "/api/admin/channels/999/test", `{}`, admin), 404)
}
func TestSSE(t *testing.T) {
	f, n := notificationFixture(t)
	cookie := loginAs(t, f.router, "admin", testPassword)
	server := httptest.NewServer(f.router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/notifications/stream", nil)
	req.AddCookie(cookie)
	resp, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(resp.Status, resp.Header)
	}
	if e = n.Senders["inapp"].Send(ctx, model.Channel{}, model.Delivery{UserID: 1, Title: "实时消息", Content: "工单提醒"}, 0); e != nil {
		t.Fatal(e)
	}
	reader := bufio.NewReader(resp.Body)
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if strings.HasPrefix(line, "data: ") {
			if !strings.Contains(line, "实时消息") {
				t.Fatal(line)
			}
			break
		}
	}
	cancel()
}
