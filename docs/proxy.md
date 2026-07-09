# Running multimux behind a reverse proxy

By default multimux terminates TLS itself using its name-constrained local CA. If
you would rather terminate TLS at a reverse proxy — for example to use a Tailscale
or Let's Encrypt certificate, or to sit behind an existing gateway — run the
daemon in proxy mode.

## `--behind-proxy`

```
multimux serve --behind-proxy
```

In this mode the daemon:

- listens on **plain HTTP bound to `127.0.0.1`** (loopback only) on its port
  (8686 by default, or `--port <n>`);
- does **not** generate or serve TLS material;
- trusts `X-Forwarded-*` headers from the proxy in front of it.

Because it binds only to loopback, nothing outside the host can reach the plain
HTTP port directly — the proxy is the only ingress. **Only put a proxy you control
in front of it**, and make sure the proxy enforces HTTPS, because the session
cookie is marked `Secure` and browsers will not send it over plain `http://`.

The proxy must forward the client's real scheme and host so that WebAuthn origin
checks and the `Secure` cookie behave correctly. Point the proxy at
`http://127.0.0.1:8686`.

## WebSocket pass-through

The terminal tiles (`/ws/pty/{id}`) and the live event stream (`/ws/events`) are
WebSockets. Your proxy **must** pass the `Upgrade` and `Connection` headers
through and not buffer these connections, or terminals will fail to connect.
Terminal sessions can idle for a long time, so set generous read/write timeouts on
the WebSocket routes.

## Worked example: Caddy

Caddy handles WebSocket upgrades transparently and can obtain a real certificate
for a public or Tailscale name automatically:

```caddyfile
mux.example.com {
    reverse_proxy 127.0.0.1:8686
}
```

That single directive terminates TLS, sets `X-Forwarded-*`, and proxies both HTTP
and WebSocket traffic. Run the daemon as `multimux serve --behind-proxy`. Make
sure `mux.example.com` (or whatever name you use) is set as the daemon's hostname
**before** registering a passkey, since it becomes the WebAuthn RP ID.

## Worked example: Tailscale Serve

[Tailscale Serve](https://tailscale.com/kb/1242/tailscale-serve/) puts the daemon
behind your tailnet's HTTPS certificate (MagicDNS name), which is an excellent
stable hostname for WebAuthn:

```
tailscale serve --bg 127.0.0.1:8686
```

This serves your daemon at `https://<host>.<tailnet>.ts.net/` with a valid
certificate and correct forwarded headers, and Tailscale proxies WebSockets
transparently. With Serve you often do not even need `multimux ca trust`, because
Tailscale's certificate is trusted by default. Set the daemon's hostname to the
MagicDNS name up front so passkeys bind to a name that will not change.
