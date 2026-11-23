# WhatsApp UnifiedPush Server

Sends an encrypted (RFC 8291 Web Push) `wake` message to your phone when a WhatsApp message is received. The app uploads its push subscription to `POST /subscription` (bearer token) and the server keeps it in SQLite.

Wakes are only sent for messages your phone will show a notification for. These are skipped:

- Messages in muted chats. In muted groups, mentions and replies to you still wake the phone unless you have also muted the sender.
- Messages in archived chats while "Keep chats archived" is on, except mentions and replies to you.
- Status updates, edits, deletions, reactions to other people's messages, removed reactions, and protocol-only messages.

Calls always wake the phone.

The user session is stored in `/data/session.db`.
SQLite stores WhatsApp linked-device/cryptographic state and the mute/archive settings used to filter wakes. No chat history is saved.

| Environment Variable | Requirement | Description / Default |
| --- | --- | --- |
| `REGISTER_TOKEN` | Optional | Secret the app uses to upload its subscription. It is saved in SQLite, so later runs can omit it. If none was ever set, a random one is generated and logged at every start |
| `PUBLIC_URL` | Optional | Public HTTPS address of this server; enables the pairing QR code |
| `LISTEN_ADDR` | Optional | Address for the subscription endpoint, defaults to `:8080`; put TLS in front of it |
| `PUSH_BEARER_TOKEN` | Optional | Bearer token to authenticate with the UnifiedPush endpoint |
| `DATA_DIR` | Optional | SQLite store, defaults to `/data` |
