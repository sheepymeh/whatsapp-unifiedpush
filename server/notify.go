package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Mirrors WhatsApp's notification rules so we only wake for messages that notify.
// When unsure, wake.

const (
	keepArchivedSetting = "keep_archived"
	keepArchivedUnknown = int32(0)
	keepArchivedOff     = int32(1)
	keepArchivedOn      = int32(2)
)

type notifier struct {
	client       *whatsmeow.Client
	db           *sql.DB
	keepArchived atomic.Int32
	syncing      atomic.Bool
}

func newNotifier(ctx context.Context, db *sql.DB, client *whatsmeow.Client) (*notifier, error) {
	n := &notifier{client: client, db: db}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS wa_up_settings (key TEXT PRIMARY KEY, value INTEGER NOT NULL)`); err != nil {
		return nil, err
	}
	var v int32
	err := db.QueryRowContext(ctx, `SELECT value FROM wa_up_settings WHERE key = ?`, keepArchivedSetting).Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	n.keepArchived.Store(v)
	// An unchanged "Keep chats archived" setting is only sent during full syncs.
	client.EmitAppStateEventsOnFullSync = true
	return n, nil
}

func (n *notifier) setKeepArchived(ctx context.Context, v int32) {
	n.keepArchived.Store(v)
	if _, err := n.db.ExecContext(ctx, `INSERT OR REPLACE INTO wa_up_settings (key, value) VALUES (?, ?)`, keepArchivedSetting, v); err != nil {
		log.Printf("Saving archive setting failed: %v", err)
	}
}

func (n *notifier) handleEvent(ctx context.Context, evt any) {
	switch e := evt.(type) {
	case *events.UnarchiveChatsSetting:
		if e.Action.GetUnarchiveChats() {
			n.setKeepArchived(ctx, keepArchivedOff)
		} else {
			n.setKeepArchived(ctx, keepArchivedOn)
		}
	case *events.AppStateSyncComplete:
		if e.Name == appstate.WAPatchRegularLow && n.keepArchived.Load() == keepArchivedUnknown {
			n.setKeepArchived(ctx, keepArchivedOff)
		}
	case *events.Connected:
		go n.syncKeepArchived(ctx)
	}
}

// syncKeepArchived refetches the setting for sessions synced before it was stored.
func (n *notifier) syncKeepArchived(ctx context.Context) {
	if n.keepArchived.Load() != keepArchivedUnknown || !n.syncing.CompareAndSwap(false, true) {
		return
	}
	defer n.syncing.Store(false)
	version, _, err := n.client.Store.AppState.GetAppStateVersion(ctx, string(appstate.WAPatchRegularLow))
	if err != nil || version == 0 {
		return // not synced yet; the initial sync will include it
	}
	if err := n.client.FetchAppState(ctx, appstate.WAPatchRegularLow, true, false); err != nil {
		log.Printf("Fetching archive settings failed: %v", err)
	}
}

func (n *notifier) shouldWakeMessage(ctx context.Context, e *events.Message) bool {
	msg := e.Message
	switch {
	case e.Info.Chat == types.StatusBroadcastJID, e.IsEdit, msg == nil, msg.GetProtocolMessage() != nil, !hasContent(msg):
		return false
	}
	if r := msg.GetReactionMessage(); r != nil {
		return r.GetText() != "" && n.targetsMe(e.Info.IsGroup, r.GetKey().GetFromMe(), r.GetKey().GetParticipant()) &&
			n.shouldWakeChat(ctx, &e.Info, false)
	}
	if r := msg.GetEncReactionMessage(); r != nil {
		return n.targetsMe(e.Info.IsGroup, r.GetTargetMessageKey().GetFromMe(), r.GetTargetMessageKey().GetParticipant()) &&
			n.shouldWakeChat(ctx, &e.Info, false)
	}
	return n.shouldWakeChat(ctx, &e.Info, n.mayBeDirect(msg))
}

func (n *notifier) shouldWakeUndecryptable(ctx context.Context, e *events.UndecryptableMessage) bool {
	// Content unknown, so treat it as a possible mention.
	return e.DecryptFailMode != events.DecryptFailHide && e.Info.Chat != types.StatusBroadcastJID &&
		n.shouldWakeChat(ctx, &e.Info, true)
}

// shouldWakeChat applies mute and archive rules. Mentions and replies (direct)
// bypass a muted group unless the sender is muted, and bypass archiving.
func (n *notifier) shouldWakeChat(ctx context.Context, info *types.MessageInfo, direct bool) bool {
	chat := []types.JID{info.Chat}
	if !info.IsGroup {
		chat = append(chat, info.SenderAlt)
	}
	muted, archived := n.chatState(ctx, chat...)
	if muted {
		if !info.IsGroup || !direct {
			return false
		}
		if senderMuted, _ := n.chatState(ctx, info.Sender, info.SenderAlt); senderMuted {
			return false
		}
	}
	return !archived || direct || n.keepArchived.Load() != keepArchivedOn
}

// chatState checks a chat under both its phone number and LID.
func (n *notifier) chatState(ctx context.Context, jids ...types.JID) (muted, archived bool) {
	for _, jid := range jids {
		if jid.IsEmpty() {
			continue
		}
		jid = jid.ToNonAD()
		alt, _ := n.client.Store.GetAltJID(ctx, jid)
		for _, j := range []types.JID{jid, alt} {
			if j.IsEmpty() {
				continue
			}
			s, err := n.client.Store.ChatSettings.GetChatSettings(ctx, j)
			if err != nil {
				log.Printf("Reading chat settings failed: %v", err)
				continue
			}
			muted = muted || s.MutedUntil.After(time.Now())
			archived = archived || s.Archived
		}
	}
	return
}

// targetsMe reports whether a reaction key, written from the reactor's point of view, refers to our message.
func (n *notifier) targetsMe(isGroup, fromMe bool, participant string) bool {
	return !fromMe && (!isGroup || n.isMe(participant))
}

// mayBeDirect reports whether msg mentions or replies to us, or might: wrapper
// messages (spoiler, group mention, ...) and unknown fields are not inspected,
// so they count as direct.
func (n *notifier) mayBeDirect(msg *waE2E.Message) bool {
	if len(msg.ProtoReflect().GetUnknown()) > 0 {
		return true
	}
	direct := false
	msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return true
		}
		part := v.Message()
		if _, wrapper := part.Interface().(*waE2E.FutureProofMessage); wrapper || len(part.GetUnknown()) > 0 {
			direct = true
			return false
		}
		field := part.Descriptor().Fields().ByName("contextInfo")
		if field == nil || !part.Has(field) {
			return true
		}
		ci, ok := part.Get(field).Message().Interface().(*waE2E.ContextInfo)
		if !ok {
			return true
		}
		direct = ci.GetNonJIDMentions() > 0 || (ci.GetStanzaID() != "" && n.isMe(ci.GetParticipant()))
		for _, jid := range ci.GetMentionedJID() {
			direct = direct || n.isMe(jid)
		}
		return !direct
	})
	return direct
}

// hasContent is false for messages carrying only group keys or metadata.
func hasContent(msg *waE2E.Message) bool {
	if len(msg.ProtoReflect().GetUnknown()) > 0 {
		return true
	}
	found := false
	msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		switch fd.Name() {
		case "senderKeyDistributionMessage", "fastRatchetKeySenderKeyDistributionMessage", "messageContextInfo":
			return true
		}
		found = true
		return false
	})
	return found
}

func (n *notifier) isMe(s string) bool {
	jid, err := types.ParseJID(s)
	if s == "" || err != nil {
		return false
	}
	jid = jid.ToNonAD()
	for _, own := range []types.JID{n.client.Store.GetJID(), n.client.Store.GetLID()} {
		if own = own.ToNonAD(); !own.IsEmpty() && jid.User == own.User && jid.Server == own.Server {
			return true
		}
	}
	return false
}
