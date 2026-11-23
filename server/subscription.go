package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sync/atomic"

	"github.com/SherClockHolmes/webpush-go"
)

const (
	subscriptionPath    = "/subscription"
	maxSubscriptionBody = 4096
	p256dhLength        = 65
	authLength          = 16
	tokenKey            = "token"
	subscriptionKey     = "subscription"
)

// subscriptions holds the subscription uploaded by the app.
type subscriptions struct {
	db        *sql.DB
	token     string
	generated bool // REGISTER_TOKEN unset: the user may not know the token
	cur       atomic.Pointer[webpush.Subscription]
}

func (s *subscriptions) get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM wa_up_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *subscriptions) set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO wa_up_state (key, value) VALUES (?, ?)`, key, value)
	return err
}

// newSubscriptions loads the stored subscription and token; a provided token replaces the stored one.
func newSubscriptions(ctx context.Context, db *sql.DB, token string) (*subscriptions, error) {
	s := &subscriptions{db: db, token: token, generated: token == ""}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS wa_up_state (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return nil, err
	}
	if s.generated {
		var err error
		if s.token, err = s.get(ctx, tokenKey); err != nil {
			return nil, err
		}
		if s.token == "" {
			s.token = rand.Text()
		}
	}
	if err := s.set(ctx, tokenKey, s.token); err != nil {
		return nil, err
	}
	data, err := s.get(ctx, subscriptionKey)
	if err != nil {
		return nil, err
	}
	var sub webpush.Subscription
	if json.Unmarshal([]byte(data), &sub) == nil && validSubscription(&sub) {
		s.cur.Store(&sub)
	}
	return s, nil
}

func validHTTPS(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

func validSubscription(sub *webpush.Subscription) bool {
	key := func(s string, n int) bool {
		b, err := base64.RawURLEncoding.DecodeString(s)
		return err == nil && len(b) == n
	}
	return validHTTPS(sub.Endpoint) && key(sub.Keys.P256dh, p256dhLength) && key(sub.Keys.Auth, authLength)
}

func (s *subscriptions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var sub webpush.Subscription
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSubscriptionBody)).Decode(&sub); err != nil || !validSubscription(&sub) {
		http.Error(w, "invalid subscription", http.StatusBadRequest)
		return
	}
	data, _ := json.Marshal(sub)
	if err := s.set(r.Context(), subscriptionKey, string(data)); err != nil {
		log.Printf("Saving subscription failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.cur.Store(&sub)
	log.Print("Subscription updated")
	w.WriteHeader(http.StatusNoContent)
}

func (s *subscriptions) serve(addr string) {
	mux := http.NewServeMux()
	mux.Handle(subscriptionPath, s)
	srv := &http.Server{Addr: addr, Handler: mux, ReadTimeout: pushRequestTimeout}
	go func() {
		log.Fatalf("Subscription listener failed: %v", srv.ListenAndServe())
	}()
}
