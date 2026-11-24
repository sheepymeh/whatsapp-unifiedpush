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
	"syscall"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const (
	defaultDataDir     = "/data"
	defaultListenAddr  = ":8080"
	sessionFilename    = "session.db"
	dataDirPermissions = 0o700
	sessionPermissions = 0o600
	maxDBConnections   = 1
	pushRequestTimeout = 12 * time.Second
	pairingRetryDelay  = 3 * time.Second
	wakePayload        = "wake"
	wakeTTLSeconds     = 60
	wakeRecordSize     = 128 // default padding is 4096 bytes
)

type config struct {
	dataDir       string
	token         string
	listenAddr    string
	registerToken string
	publicURL     string
}

// authClient swaps webpush-go's VAPID Authorization header for the bearer token, if set.
type authClient struct {
	*http.Client
	token string
}

func (c authClient) Do(req *http.Request) (*http.Response, error) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.Client.Do(req)
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func loadConfig() (config, error) {
	cfg := config{
		dataDir:       strings.TrimSpace(os.Getenv("DATA_DIR")),
		token:         os.Getenv("PUSH_BEARER_TOKEN"),
		listenAddr:    strings.TrimSpace(os.Getenv("LISTEN_ADDR")),
		registerToken: os.Getenv("REGISTER_TOKEN"),
		publicURL:     strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_URL")), "/"),
	}
	if cfg.dataDir == "" {
		cfg.dataDir = defaultDataDir
	}
	if cfg.listenAddr == "" {
		cfg.listenAddr = defaultListenAddr
	}
	if cfg.publicURL != "" && !validHTTPS(cfg.publicURL) {
		return config{}, errors.New("PUBLIC_URL must be an HTTPS URL")
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
	subs, err := newSubscriptions(ctx, db, cfg.registerToken)
	if err != nil {
		return fmt.Errorf("initialize subscription: %w", err)
	}
	subs.serve(cfg.listenAddr)
	if subs.cur.Load() == nil || subs.generated {
		printPairing(cfg.publicURL, subs.token)
	}
	wake := newWaker(func(ctx context.Context, kind wakeKind) error {
		sub := subs.cur.Load()
		if sub == nil {
			return errNoSubscription
		}
		return sendWake(ctx, httpClient, cfg.token, subs.vapidPriv, subs.vapidPub, sub, kind)
	})

	client := whatsmeow.NewClient(device, waLog.Stdout("whatsmeow", "INFO", false))
	client.EnableAutoReconnect = true
	client.InitialAutoReconnect = true
	notify, err := newNotifier(ctx, db, client)
	if err != nil {
		return fmt.Errorf("initialize settings: %w", err)
	}
	client.AddEventHandler(func(evt any) {
		notify.handleEvent(ctx, evt)
		switch e := evt.(type) {
		case *events.Message:
			if isLinkPreviewRequest(e.Message) {
				// Logged to see whether WhatsApp ever delivers this.
				log.Printf("Link preview request from %s", e.Info.Sender)
				wake.message(ctx)
			} else if !e.Info.IsFromMe && e.SourceWebMsg == nil && notify.shouldWakeMessage(ctx, e) {
				wake.message(ctx)
			}
		case *events.UndecryptableMessage:
			if !e.Info.IsFromMe && notify.shouldWakeUndecryptable(ctx, e) {
				wake.message(ctx)
			}
		case *events.CallOffer, *events.CallOfferNotice:
			wake.call(ctx)
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

// printPairing shows how to pair the app. A generated token is shown at every start, as it cannot be looked up elsewhere.
func printPairing(publicURL, token string) {
	if publicURL == "" {
		log.Printf("No subscription yet. In the app, enter your server URL ending in %s and token %s", subscriptionPath, token)
		log.Print("Set PUBLIC_URL to also show a QR code for pairing")
		return
	}
	endpoint := publicURL + subscriptionPath
	link := "wa-up://pair?" + url.Values{"url": {endpoint}, "token": {token}}.Encode()
	log.Printf("No subscription yet. Scan this QR code with your phone's camera, or enter in the app:\n%sServer URL: %s\nToken: %s", qrText(link), endpoint, token)
}

// qrText renders a QR code as text so it is logged in one write; other output cannot split it.
func qrText(content string) string {
	var b strings.Builder
	qrterminal.GenerateHalfBlock(content, qrterminal.L, &b)
	return b.String()
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
				log.Printf("Scan QR with WhatsApp > Linked devices (valid up to %s)\n%s", item.Timeout.Round(time.Second), qrText(item.Code))
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

// sendWake signs with a VAPID key, which UnifiedPush doesn't require but webpush-go does.
func sendWake(ctx context.Context, client *http.Client, token, vapidPriv, vapidPub string, sub *webpush.Subscription, kind wakeKind) error {
	resp, err := webpush.SendNotificationWithContext(ctx, []byte(wakePayload), sub, &webpush.Options{
		HTTPClient:      authClient{client, token},
		TTL:             wakeTTLSeconds,
		RecordSize:      wakeRecordSize,
		Urgency:         kind.urgency,
		Topic:           kind.name,
		Subscriber:      "https://github.com/sheepymeh/whatsapp-unifiedpush",
		VAPIDPublicKey:  vapidPub,
		VAPIDPrivateKey: vapidPriv,
	})
	if err != nil {
		return errors.New("could not send push request")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("push server returned HTTP %d", resp.StatusCode)
	}
	log.Printf("Wake sent (%s)", kind.name)
	return nil
}
