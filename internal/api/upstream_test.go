package api

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
	"github.com/reputationly/ticket-hub/internal/collector"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/mailwatch"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/syncer"
	"github.com/reputationly/ticket-hub/internal/ticket"
	"github.com/reputationly/ticket-hub/internal/upstream"
)

func TestUpstreamManagement(t *testing.T) {
	f, n := notificationFixture(t)
	if e := collector.Seed(f.db); e != nil {
		t.Fatal(e)
	}
	cipher, _ := crypto.New(f.cfg.Secret)
	hub := mailwatch.New(f.db, cipher)
	accounts := &upstream.Service{DB: f.db, Cache: f.store, Cipher: cipher, Waiter: hub, DevMode: true}
	scheduler := syncer.New(f.db, f.store, collector.NewRegistry(), accounts, &ticket.Service{DB: f.db, Cache: f.store, Dispatcher: n}, true)
	f.router = NewWithUpstream(f.db, f.store, f.cfg, n, hub, accounts, scheduler)
	admin := loginAs(t, f.router, "admin", testPassword)
	create(t, f, admin, "upstream_member", "user")
	member := loginAs(t, f.router, "upstream_member", testPassword)
	for _, path := range []string{"/api/admin/mail-accounts", "/api/admin/upstream", "/api/admin/collectors", "/api/admin/mail-rules", "/api/admin/sync-logs"} {
		status(t, request(t, f.router, "GET", path, "", member), 403)
		status(t, request(t, f.router, "GET", path, "", admin), 200)
	}
	srv := server.New(memory.New())
	srv.AllowInsecureAuth = true
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	go srv.Serve(listener)
	defer srv.Close()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	p, _ := strconv.Atoi(port)
	body := fmt.Sprintf(`{"name":"收件箱","host":%q,"port":%d,"tls":"none","username":"username","password":"password","enabled":true}`, host, p)
	w := request(t, f.router, "POST", "/api/admin/mail-accounts", body, admin)
	status(t, w, 200)
	id := responseID(t, w)
	if strings.Contains(w.Body.String(), `"password":"password"`) {
		t.Fatal("授权码泄漏")
	}
	path := fmt.Sprintf("/api/admin/mail-accounts/%d", id)
	status(t, request(t, f.router, "POST", path+"/test", `{}`, admin), 200)
	var saved model.MailAccount
	f.db.First(&saved, id)
	if saved.Password == "password" {
		t.Fatal("明文保存")
	}
	body = fmt.Sprintf(`{"name":"更新","host":%q,"port":%d,"tls":"none","username":"username","password":"********","enabled":true}`, host, p)
	status(t, request(t, f.router, "PUT", path, body, admin), 200)
	w = request(t, f.router, "POST", "/api/admin/mail-rules", fmt.Sprintf(`{"name":"触发维修","mail_account_id":%d,"from":"sender@","subject":"工单","kind":"fault_repair","enabled":true}`, id), admin)
	status(t, w, 200)
	rid := responseID(t, w)
	status(t, request(t, f.router, "DELETE", path, "", admin), 409)
	w = request(t, f.router, "POST", "/api/admin/upstream", `{"name":"模拟登录","type":"mock","email":"mock@example.com","enabled":true}`, admin)
	status(t, w, 200)
	uid := responseID(t, w)
	up := fmt.Sprintf("/api/admin/upstream/%d", uid)
	w = request(t, f.router, "POST", up+"/login", `{}`, admin)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"state":"在线"`) {
		t.Fatal(w.Body.String())
	}
	status(t, request(t, f.router, "GET", up+"/status", "", admin), 200)
	status(t, request(t, f.router, "POST", up+"/manual-code", `{"code":"123456"}`, admin), 400)
	status(t, request(t, f.router, "PUT", "/api/admin/collectors/fault_repair", `{"enabled":true,"mock":true,"interval_seconds":10,"work_interval_seconds":10,"off_interval_seconds":10,"filters":{"site":"联调机房"}}`, admin), 200)
	status(t, request(t, f.router, "POST", "/api/admin/collectors/fault_repair/sync", `{}`, admin), 200)
	if _, e = scheduler.Sync(context.Background(), "fault_repair", "test"); e != nil {
		t.Fatal(e)
	}
	w = request(t, f.router, "GET", "/api/tickets?kind=fault_repair", "", member)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), "联调机房") {
		t.Fatal(w.Body.String())
	}
	status(t, request(t, f.router, "GET", "/api/admin/sync-logs?kind=fault_repair&status=success", "", admin), 200)
	status(t, request(t, f.router, "PUT", "/api/admin/collectors/fault_repair", `{"enabled":true,"mock":true,"interval_seconds":1}`, admin), 400)
	status(t, request(t, f.router, "POST", "/api/admin/mail-rules", fmt.Sprintf(`{"name":"坏规则","mail_account_id":%d,"from":"[","kind":"fault_repair"}`, id), admin), 400)
	status(t, request(t, f.router, "POST", "/api/admin/upstream", `{"name":"未确认","type":"dora","email":"x@example.com","enabled":true}`, admin), 400)
	status(t, request(t, f.router, "DELETE", fmt.Sprintf("/api/admin/mail-rules/%d", rid), "", admin), 200)
	status(t, request(t, f.router, "DELETE", path, "", admin), 200)
	status(t, request(t, f.router, "DELETE", up, "", admin), 200)
	cfg := f.cfg
	cfg.DevMode = false
	prod := NewWithUpstream(f.db, f.store, cfg, n, hub, accounts, scheduler)
	status(t, request(t, prod, "POST", "/api/admin/upstream", `{"name":"模拟","type":"mock","email":"mock@example.com"}`, admin), 400)
}
