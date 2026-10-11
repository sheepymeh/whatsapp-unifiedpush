# WhatsApp UnifiedPush

More reliable WhatsApp notifications without GMS. Wake WhatsApp Android through [UnifiedPush](https://unifiedpush.org/) using a lightweight [whatsmeow](https://github.com/tulir/whatsmeow) linked-device server.

## Quickstart

1. Download the APK from [GitHub Releases](https://github.com/sheepymeh/whatsapp-unifiedpush/releases) and install it.
2. Open the app to register with a UnifiedPush distributor, and copy the endpoint URL.
3. Start the server:

   ```sh
   docker run -d --name wa-up --restart unless-stopped \
     -e PUSH_ENDPOINT='https://your-push-endpoint' \
     -v wa-up-data:/data \
     ghcr.io/sheepymeh/whatsapp-unifiedpush:latest
   ```

4. Run `docker logs -f wa-up` and scan the QR code in WhatsApp → Linked devices.

The server sends only `wake`, not the contents of your messages.

It uses a raw HTTP POST to the endpoint, so the chosen distributor must support this delivery method (e.g. ntfy).

Unofficial project; not affiliated with WhatsApp. WhatsApp protocol changes or account restrictions may affect operation.
