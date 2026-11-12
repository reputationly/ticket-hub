package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/reputationly/ticket-hub/internal/cache"
	"github.com/reputationly/ticket-hub/internal/crypto"
	"github.com/reputationly/ticket-hub/internal/model"
	"github.com/reputationly/ticket-hub/internal/queue"
	"github.com/reputationly/ticket-hub/internal/sender"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct {
	DB        *gorm.DB
	Cache     cache.Cache
	Cipher    *crypto.Cipher
	Queue     queue.Queue
	Senders   sender.Registry
	PublicURL func(context.Context) (string, error)
	Now       func() time.Time
}

func New(d *gorm.DB, c cache.Cache, cipher *crypto.Cipher, q queue.Queue, senders sender.Registry) *Service {
	return &Service{DB: d, Cache: c, Cipher: cipher, Queue: q, Senders: senders, Now: time.Now}
}

type target struct {
	user    model.User
	channel model.Channel
	source  string
	group   bool
}

func (s *Service) Dispatch(ctx context.Context, eventID uint) error {
	now := s.Now()
	var jobs []model.NotifyJob
	publicURL := ""
	if s.PublicURL != nil {
		v, e := s.PublicURL(ctx)
		if e != nil {
			return e
		}
		publicURL = v
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event model.TicketEvent
		if e := tx.First(&event, eventID).Error; e != nil {
			return e
		}
		if event.Dispatched {
			return nil
		}
		var ticket model.Ticket
		if e := json.Unmarshal(event.After, &ticket); e != nil {
			return e
		}
		var rules []model.NotifyRule
		var subs []model.Subscription
		var users []model.User
		var channels []model.Channel
		if e := tx.Where("enabled = ?", true).Order("id ASC").Find(&rules).Error; e != nil {
			return e
		}
		if e := tx.Find(&subs).Error; e != nil {
			return e
		}
		if e := tx.Where("enabled = ?", true).Find(&users).Error; e != nil {
			return e
		}
		if e := tx.Where("enabled = ?", true).Order("id ASC").Find(&channels).Error; e != nil {
			return e
		}
		byUser := map[uint]model.User{}
		bySub := map[uint]model.Subscription{}
		byChannel := map[uint]model.Channel{}
		for _, u := range users {
			byUser[u.ID] = u
		}
		for _, sub := range subs {
			bySub[sub.UserID] = sub
		}
		for _, ch := range channels {
			byChannel[ch.ID] = ch
		}
		targets := map[string]target{}
		mentioned := map[uint]model.User{}
		add := func(userID uint, types []string, source string) {
			u, ok := byUser[userID]
			if !ok {
				return
			}
			sub := bySub[userID]
			quiet := Quiet(sub, now, Urgent(ticket))
			key := fmt.Sprintf("u:%d:c:0", userID)
			if _, ok := targets[key]; !ok {
				targets[key] = target{user: u, channel: model.Channel{Type: "inapp"}, source: source}
			}
			if quiet {
				return
			}
			for _, typ := range types {
				if typ == "sms" {
					typ = "sms_aliyun"
				}
				if typ == "inapp" {
					continue
				}
				if typ == "sms_aliyun" && event.Type != "ticket.created" && !Urgent(ticket) {
					continue
				}
				if typ != "email" && typ != "sms_aliyun" {
					continue
				}
				if typ == "email" && u.Email == "" || typ == "sms_aliyun" && u.Phone == "" {
					continue
				}
				for _, ch := range channels {
					if ch.Type != typ {
						continue
					}
					key := fmt.Sprintf("u:%d:c:%d", userID, ch.ID)
					if _, ok := targets[key]; !ok {
						targets[key] = target{user: u, channel: ch, source: source}
					}
				}
			}
		}
		for _, rule := range rules {
			if !Matches(rule.Conditions, ticket, event) {
				continue
			}
			for _, id := range rule.Actions.UserIDs {
				add(id, rule.Actions.PersonalChannels, rule.Actions.Template)
			}
			for _, id := range rule.Actions.GroupChannelIDs {
				ch, ok := byChannel[id]
				if !ok || ch.Type != "wecom_bot" && ch.Type != "feishu_bot" {
					continue
				}
				key := fmt.Sprintf("g:%d", id)
				if _, ok := targets[key]; !ok {
					targets[key] = target{channel: ch, source: rule.Actions.Template, group: true}
				}
			}
		}
		for _, sub := range subs {
			if !sub.Enabled || !Matches(model.Conditions{Kinds: sub.Kinds, Sites: sub.Sites, EventTypes: sub.EventTypes}, ticket, event) {
				continue
			}
			add(sub.UserID, sub.Channels, "")
			if sub.MentionInGroup && !Quiet(sub, now, Urgent(ticket)) {
				if u, ok := byUser[sub.UserID]; ok {
					mentioned[u.ID] = u
				}
			}
		}
		keys := make([]string, 0, len(targets))
		for key := range targets {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		mentionIDs := make([]int, 0, len(mentioned))
		for id := range mentioned {
			mentionIDs = append(mentionIDs, int(id))
		}
		sort.Ints(mentionIDs)
		for _, key := range keys {
			dest := targets[key]
			content, e := Render(dest.source, ticket, event, publicURL)
			if e != nil {
				return e
			}
			delivery := model.Delivery{UserID: dest.user.ID, TicketID: ticket.ID, Title: ticket.Title, Content: content, Phone: dest.user.Phone, Email: dest.user.Email, OpenID: dest.user.FeishuOpenID, Variables: map[string]string{"title": ticket.Title, "status": ticket.Status, "kind": ticket.Kind, "site": ticket.Site, "upstream_id": ticket.UpstreamID, "node": ticket.Node, "assignee": ticket.Assignee, "priority": ticket.Priority, "event": event.Type, "link": TemplateLink(publicURL, ticket.ID)}}
			if dest.group {
				for _, id := range mentionIDs {
					u := mentioned[uint(id)]
					if u.Phone != "" {
						delivery.MentionMobiles = append(delivery.MentionMobiles, u.Phone)
					}
					if u.FeishuOpenID != "" {
						delivery.MentionOpenIDs = append(delivery.MentionOpenIDs, u.FeishuOpenID)
					} else {
						name := u.DisplayName
						if name == "" {
							name = u.Username
						}
						delivery.MentionNames = append(delivery.MentionNames, name)
					}
				}
			}
			job := model.NotifyJob{DedupeKey: fmt.Sprintf("e:%d:%s", event.ID, key), EventID: event.ID, UserID: dest.user.ID, ChannelID: dest.channel.ID, ChannelType: dest.channel.Type, Delivery: delivery, Status: "pending", NextAttemptAt: now}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&job)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				jobs = append(jobs, job)
			}
		}
		return tx.Model(&model.TicketEvent{}).Where("id = ?", event.ID).Update("dispatched", true).Error
	})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if e := s.Queue.Push(ctx, job.ID); e != nil {
			return e
		}
	}
	return nil
}
func TemplateLink(publicURL string, id uint) string {
	return fmt.Sprintf("%s/tickets/%d", strings.TrimRight(publicURL, "/"), id)
}
