# WhatsApp UnifiedPush

More reliable WhatsApp notifications without GMS. Wake WhatsApp Android through [UnifiedPush](https://unifiedpush.org/) using a lightweight [whatsmeow](https://github.com/tulir/whatsmeow) linked-device server.

## Quickstart

1. Download the APK from [GitHub Releases](https://github.com/sheepymeh/whatsapp-unifiedpush/releases) and install it.
2. Install a UnifiedPush distributor.
3. Set WhatsApp's battery usage to **Unrestricted** (Settings → Apps → WhatsApp → Battery). With "Optimized", Android blocks WhatsApp's network access and background starts when woken by this app, so notifications may not arrive.
4. Start the server with an HTTPS reverse proxy or tunnel:

   ```sh
   docker run -d --name wa-up --restart unless-stopped \
     -e PUBLIC_URL='https://wa-up.example.com' \
     -p 127.0.0.1:8080:8080 \
     -v wa-up-data:/data \
     ghcr.io/sheepymeh/whatsapp-unifiedpush:latest
   ```

5. Run `docker logs -f wa-up`.
6. Scan the first QR code (for subscriptions) with your phone's camera to pair the app (or enter the server URL and token shown below the code in the app).
7. Scan the WhatsApp QR code in WhatsApp → Linked devices.

The app uploads its push subscription to the server, and again whenever the endpoint changes. The server sends only `wake`, not the contents of your messages, as an encrypted Web Push message.

Unofficial project; not affiliated with WhatsApp. WhatsApp protocol changes or account restrictions may affect operation.
