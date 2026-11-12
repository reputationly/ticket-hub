package api

import (
	"github.com/gin-gonic/gin"
	"github.com/reputationly/ticket-hub/internal/collector"
	"github.com/reputationly/ticket-hub/internal/model"
)

func (s *Server) publicSite(c *gin.Context) {
	cfg, e := s.settings(c.Request.Context())
	if e != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"site_name": cfg.SiteName, "logo_url": cfg.LogoURL})
}
func (s *Server) catalog(c *gin.Context) {
	kinds := make([]gin.H, 0, len(collector.Kinds))
	for _, k := range collector.Kinds {
		kinds = append(kinds, gin.H{"value": k.Kind, "label": k.Name})
	}
	sites, statuses := []string{}, []string{}
	if e := s.db.WithContext(c.Request.Context()).Model(&model.Ticket{}).Distinct("site").Where("site <> ?", "").Order("site").Pluck("site", &sites).Error; e != nil {
		internal(c)
		return
	}
	if e := s.db.WithContext(c.Request.Context()).Model(&model.Ticket{}).Distinct("status").Where("status <> ?", "").Order("status").Pluck("status", &statuses).Error; e != nil {
		internal(c)
		return
	}
	ok(c, gin.H{"kinds": kinds, "sites": sites, "statuses": statuses})
}
