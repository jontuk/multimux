# Android: trusting the multimux CA

Android needs the daemon's CA in its user trust store before Chrome can create
or use passkeys, or install the multimux PWA. On first run the daemon prints an
**Android trust** URL and the CA's full SHA-256 fingerprint.

## Guided install (LAN, VPN, or tailnet you control only)

1. Bypass Chrome's certificate warning once and open the printed `/trust` URL.
   Do not do this on a public or otherwise untrusted network.
2. Tap **Download CA certificate**. It downloads as `multimux-ca.crt`.
3. Install it:
   - **Pixel / stock Android:** Settings → Security & privacy → More security
     settings → Encryption & credentials → Install a certificate → CA certificate.
   - **Samsung:** Settings → Security and privacy → More security settings →
     Credential storage → Install from device storage → CA certificate.
4. In Android's certificate details, compare the SHA-256 fingerprint with the
   one in the daemon's console or service log — the untrusted web page is not
   an authenticated reference. **Remove the certificate if they don't match.**
5. Back in Chrome, tap **Reload and check trust**. Chrome caches the failed TLS
   handshake, so if it still reports untrusted, force-stop Chrome, reopen it,
   and reload. Then continue to the setup or login URL.

`/ca.crt` and `/ca/info` are public so a phone can bootstrap trust. They expose
only the public certificate; `pki/ca.key` is never exported.

## Manual install

If Chrome won't open the untrusted page, or you don't fully control the
network, fetch the CA over an already-trusted SSH connection and transfer it to
the phone by USB or Quick Share:

```sh
scp user@daemon-host:~/.local/share/multimux/pki/ca.pem multimux-ca.crt
```

## Removing / rotating

Remove via Settings → … → **User credentials** (Pixel) or **User certificates**
(Samsung); OEM labels vary, so Settings search helps. When the daemon
regenerates its CA (hostname set changed, or near expiry), repeat the install
and fingerprint check on every phone, then remove the old CA.

Managed-device policy may forbid user-added CAs entirely; multimux cannot work
around that.
