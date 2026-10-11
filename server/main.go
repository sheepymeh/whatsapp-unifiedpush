package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	defaultDataDir        = "/data"
	sessionFilename       = "session.db"
	dataDirPermissions    = 0o700
	sessionPermissions    = 0o600
	maxDBConnections      = 1
	wakeCooldown          = 60 * time.Second
	pushRequestTimeout    = 12 * time.Second
	pairingRetryDelay     = 3 * time.Second
	wakePayload           = "wake"
)

type config struct {
	endpoint string
	dataDir  string
	token    string
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func loadConfig() (config, error) {
	cfg := config{
		endpoint: strings.TrimSpace(os.Getenv("PUSH_ENDPOINT")),
		dataDir:  strings.TrimSpace(os.Getenv("DATA_DIR")),
		token:    os.Getenv("PUSH_BEARER_TOKEN"),
	}
	if cfg.dataDir == "" {
		cfg.dataDir = defaultDataDir
	}

	u, err := url.Parse(cfg.endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return config{}, errors.New("PUSH_ENDPOINT must be an HTTPS URL")
	}
	return cfg, nil
}

func openSessionDB(dir string) (*sql.DB, error) {
	if err := os.MkdirAll(dir, dataDirPermissions); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, sessionFilename)
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxDBConnections)
	for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA journal_mode = DELETE"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := os.Chmod(path, sessionPermissions); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	db, err := openSessionDB(cfg.dataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := sqlstore.NewWithDB(db, "sqlite3", nil)
	if err := store.Upgrade(ctx); err != nil {
		return fmt.Errorf("initialize session: %w", err)
	}
	device, err := store.GetFirstDevice(ctx)
	if err != nil {
		return err
	}

	httpClient := &http.Client{
		Timeout: pushRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	var lastWake atomic.Int64
	wake := func() {
		now := time.Now().Unix()
		if now-lastWake.Load() < int64(wakeCooldown/time.Second) {
			return
		}
		lastWake.Store(now)
		go func() {
			if err := sendWake(ctx, httpClient, cfg); err != nil {
				log.Printf("Wake failed: %v", err)
			}
		}()
	}

	client := whatsmeow.NewClient(device, nil)
	client.EnableAutoReconnect = true
	client.InitialAutoReconnect = true
	client.AddEventHandler(func(evt any) {
		switch e := evt.(type) {
		case *events.Message:
			if !e.Info.IsFromMe && e.SourceWebMsg == nil {
				wake()
			}
		case *events.UndecryptableMessage:
			if !e.Info.IsFromMe {
				wake()
			}
		case *events.CallOffer, *events.CallOfferNotice:
			wake()
		case *events.LoggedOut:
			log.Print("Linked device logged out; remove session.db to pair again")
		}
	})

	if device.ID == nil {
		if err := pair(ctx, client); err != nil {
			return err
		}
	} else if err := client.Connect(); err != nil {
		return err
	}
	defer client.Disconnect()
	log.Print("WhatsApp connected; listening for messages")
	<-ctx.Done()
	return nil
}

func pair(ctx context.Context, client *whatsmeow.Client) error {
	for ctx.Err() == nil {
		qr, err := client.GetQRChannel(ctx)
		if err != nil {
			return err
		}
		if err := client.Connect(); err != nil {
			return err
		}
		for item := range qr {
			switch item.Event {
			case "code":
				log.Printf("Scan QR with WhatsApp > Linked devices (valid up to %s)", item.Timeout.Round(time.Second))
				qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, os.Stdout)
			case "success":
				log.Print("WhatsApp linked successfully")
				return nil
			case "error":
				log.Printf("Pairing: %v", item.Error)
			}
		}
		if client.Store.ID != nil {
			return nil
		}
		client.Disconnect()
		log.Print("QR pairing window expired; requesting new codes")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pairingRetryDelay):
		}
	}
	return ctx.Err()
}

func sendWake(ctx context.Context, client *http.Client, cfg config) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.endpoint, strings.NewReader(wakePayload))
	if err != nil {
		return errors.New("could not create push request")
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("X-UnifiedPush", "1")
	if cfg.token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("HTTP transport error")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("push server returned HTTP %d", resp.StatusCode)
	}
	log.Print("Wake sent")
	return nil
}
