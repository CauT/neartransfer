# NearTransfer 0.1.0 — Development Preview

Share text, photos, and files across your local network. Open the Mac app, scan its QR code on another device, and share in both directions through a browser. No account or cloud service required.

## Download and install

**Apple Silicon (arm64), macOS 13 or later.**

- **DMG:** open `NearTransfer-0.1.0-dev-arm64.dmg` and drag **NearTransfer.app** into **Applications**.
- **ZIP:** extract the archive and open **NearTransfer.app**.
- The DMG contains the same app as the existing ZIP. SHA-256 files are included with the downloads.

Keep the Mac app running and connect both devices to the same Wi-Fi/LAN. The app opens a local sharing page in your default browser.

## Included

- Text sharing and copying, image previews, multiple file uploads, and an automatically refreshed inbox.
- Original file bytes and names, up to 1 GiB per item; duplicate names do not overwrite existing uploads.
- Fresh pairing credentials on every launch, persistent local inbox, and service shutdown when the app quits.

![NearTransfer desktop interface](https://raw.githubusercontent.com/CauT/neartransfer/main/docs/screenshots/desktop.jpg)

## Preview notes

This is an ad-hoc signed development preview without Developer ID signing or Apple notarization. Transfers use unencrypted HTTP and should be used on a trusted LAN. Pairing links grant access to the current inbox.

Safari image and binary downloads have been checked against the original SHA-256 hashes. Automated Chrome downloads reported `ERR_BLOCKED_BY_CLIENT`. Physical phone pairing and saving remain unverified; this release is published without waiting for additional device acceptance testing.

See the [README](https://github.com/CauT/neartransfer#readme) and [acceptance record](https://github.com/CauT/neartransfer/blob/main/docs/acceptance.md) for details.
