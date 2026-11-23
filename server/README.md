# WhatsApp UnifiedPush Server

Sends a raw HTTP `wake` POST when a WhatsApp message is received to `PUSH_ENDPOINT`.

Wakes are only sent for messages your phone will show a notification for. These are skipped:

- Messages in muted chats. In muted groups, mentions and replies to you still wake the phone unless you have also muted the sender.
- Messages in archived chats while "Keep chats archived" is on, except mentions and replies to you.
- Status updates, edits, deletions, reactions to other people's messages, removed reactions, and protocol-only messages.

Calls always wake the phone.

The user session is stored in `/data/session.db`.
SQLite stores WhatsApp linked-device/cryptographic state and the mute/archive settings used to filter wakes. No chat history is saved.

| Environment Variable | Requirement | Description / Default |
| --- | --- | --- |
| `PUSH_ENDPOINT` | Required | HTTPS UnifiedPush endpoint |
| `PUSH_BEARER_TOKEN` | Optional | Bearer token to authenticate with UnifiedPush endpoint |
| `DATA_DIR` | Optional | SQLite store, defaults to `/data` |
