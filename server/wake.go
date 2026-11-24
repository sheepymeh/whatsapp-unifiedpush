package main

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/SherClockHolmes/webpush-go"
)

// wakeKind sets how a wake is delivered. The name doubles as the Web Push topic,
// so a queued message wake can't replace a queued call wake.
type wakeKind struct {
	name    string
	urgency webpush.Urgency
}

var (
	messageWake = wakeKind{"message", webpush.UrgencyNormal}
	callWake    = wakeKind{"call", webpush.UrgencyHigh}
)

// WhatsApp stays connected after a wake and fetches later messages itself.
const messageCooldown = 60 * time.Second

var errNoSubscription = errors.New("no subscription yet; pair the app first")

// waker coalesces message wakes. Calls skip that cooldown.
type waker struct {
	send func(context.Context, wakeKind) error

	mu          sync.Mutex
	lastMessage time.Time
}

func newWaker(send func(context.Context, wakeKind) error) *waker {
	return &waker{send: send}
}

func (w *waker) message(ctx context.Context) {
	release, ok := w.claimMessage()
	if !ok {
		return
	}
	go func() {
		if !w.deliver(ctx, messageWake) {
			release()
		}
	}()
}

func (w *waker) call(ctx context.Context) {
	go w.deliver(ctx, callWake)
}

func (w *waker) deliver(ctx context.Context, kind wakeKind) bool {
	if err := w.send(ctx, kind); err != nil {
		log.Printf("Wake failed (%s): %v", kind.name, err)
		return false
	}
	return true
}

// claimMessage starts the cooldown; release undoes it after a failed push.
func (w *waker) claimMessage() (release func(), ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if !w.lastMessage.IsZero() && now.Sub(w.lastMessage) < messageCooldown {
		return nil, false
	}
	prev := w.lastMessage
	w.lastMessage = now
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.lastMessage.Equal(now) {
			w.lastMessage = prev
		}
	}, true
}
