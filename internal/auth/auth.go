package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/reputationly/ticket-hub/internal/model"
)

const CookieName = "ticket_hub_session"

type Claims struct {
	Version uint `json:"version"`
	jwt.RegisteredClaims
}
type Service struct{ key []byte }

func New(secret string) *Service {
	key := sha256.Sum256([]byte("ticket-hub:jwt:v1:" + secret))
	return &Service{key: key[:]}
}
func (s *Service) Issue(u model.User, ttl time.Duration) (string, *Claims, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	now := time.Now()
	c := &Claims{Version: u.AuthVersion, RegisteredClaims: jwt.RegisteredClaims{Issuer: "ticket-hub", Subject: strconv.FormatUint(uint64(u.ID), 10), ID: hex.EncodeToString(b), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl))}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
	return token, c, err
}
func (s *Service) Parse(raw string) (*Claims, error) {
	c := &Claims{}
	token, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) { return s.key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("ticket-hub"), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || c.ID == "" || c.Subject == "" {
		return nil, errors.New("会话无效")
	}
	return c, nil
}
