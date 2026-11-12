package api

import (
	"strings"
	"testing"

	"github.com/reputationly/ticket-hub/internal/model"
)

func TestFrontendMetadata(t *testing.T) {
	f := setup(t)
	w := request(t, f.router, "GET", "/api/site", "", nil)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), "session_hours") || !strings.Contains(w.Body.String(), "工单中心") {
		t.Fatal(w.Body.String())
	}
	status(t, request(t, f.router, "GET", "/api/catalog", "", nil), 401)
	admin := loginAs(t, f.router, "admin", testPassword)
	status(t, request(t, f.router, "PUT", "/api/admin/settings", `{"site_name":"测试工作台","logo_url":"https://example.com/logo.svg","public_url":"https://private.example.com","session_hours":24}`, admin), 200)
	w = request(t, f.router, "GET", "/api/site", "", nil)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), "测试工作台") || !strings.Contains(w.Body.String(), "logo.svg") || strings.Contains(w.Body.String(), "private.example.com") {
		t.Fatal(w.Body.String())
	}
	w = request(t, f.router, "GET", "/api/catalog", "", admin)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"sites":[]`) {
		t.Fatal(w.Body.String())
	}
	if e := f.db.Create(&model.Ticket{Kind: "fault_repair", UpstreamID: "CAT-1", Title: "目录", Site: "测试机房", Status: "待认领"}).Error; e != nil {
		t.Fatal(e)
	}
	if e := f.db.Create(&model.Ticket{Kind: "fault_repair", UpstreamID: "CAT-2", Title: "无机房工单", Status: "待检查"}).Error; e != nil {
		t.Fatal(e)
	}
	w = request(t, f.router, "GET", "/api/catalog", "", admin)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), "测试机房") || !strings.Contains(w.Body.String(), "故障维修") || !strings.Contains(w.Body.String(), "待检查") {
		t.Fatal(w.Body.String())
	}
	create(t, f, admin, "catalog-user", "user")
	user := loginAs(t, f.router, "catalog-user", testPassword)
	status(t, request(t, f.router, "GET", "/api/catalog", "", user), 200)
}

func TestPublicSiteCacheFailure(t *testing.T) {
	f := setup(t)
	_ = f.store.Close()
	status(t, request(t, f.router, "GET", "/api/site", "", nil), 500)
}
