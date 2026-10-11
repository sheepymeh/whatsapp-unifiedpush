# WhatsApp UnifiedPush Server

Sends a raw HTTP `wake` POST when a WhatsApp message is received to `PUSH_ENDPOINT`.

The user session is stored in `/data/session.db`.
SQLite stores WhatsApp linked-device/cryptographic state only, no chat history is saved.

| Environment Variable | Requirement | Description / Default |
| --- | --- | --- |
| `PUSH_ENDPOINT` | Required | HTTPS UnifiedPush endpoint |
| `PUSH_BEARER_TOKEN` | Optional | Bearer token to authenticate with UnifiedPush endpoint |
| `DATA_DIR` | Optional | SQLite store, defaults to `/data` |
