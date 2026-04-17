# Tunnel

A Go library for building peer-to-peer applications with NAT traversal and HTTP/2 transport.

## How it works

```
UDP Hole Punching → QUIC → HTTP/2
```

1. Both peers exchange NAT info via a signal server
2. UDP hole punching establishes a direct connection through NAT
3. QUIC provides encrypted, multiplexed transport over the UDP connection
4. HTTP/2 runs over QUIC streams — both peers are symmetric, each can send requests and register handlers

## Features

- **NAT traversal** — supports Full Cone, Restricted Cone, Port Restricted Cone, and Symmetric NAT (via birthday attack)
- **LAN shortcut** — automatically prefers the local network path when both peers are on the same LAN
- **Symmetric peers** — no server/client distinction; both sides get an `http.Client` and can register `http.Handler`
- **Cloudflare Worker signal** — built-in signaling via a Cloudflare Worker + KV, no infrastructure needed

## Quick start

### 1. Deploy the signal server

```bash
cd worker
# Fill in your KV namespace ID in wrangler.toml, then:
wrangler kv namespace create SIGNAL_KV
wrangler deploy
```

### 2. Run the example chat app

```bash
# First peer — prints a room token
go run ./example -name=Alice -signal=cloudflare -worker=https://<your-worker>.workers.dev

# Second peer — joins with the token
go run ./example -name=Bob -signal=cloudflare -worker=https://<your-worker>.workers.dev -room=<token>
```

Both peers connect directly. Type a message and press Enter to chat.

## API

```go
// Connect establishes a P2P connection using the provided signal.
t, _ := tunnel.NewTunnel(ctx, signal)
t.Connect()

// ConnectHTTP2 upgrades the connection to HTTP/2.
// Returns a *Peer — both sides are symmetric.
peer, _ := t.ConnectHTTP2()

// Register a handler (served to the remote peer)
peer.Handle("/hello", func(w http.ResponseWriter, r *http.Request) {
    fmt.Fprint(w, "hello!")
})

// Send a request to the remote peer
resp, _ := peer.Client.Get("https://tunnel/hello")
```

### Signal interface

Implement `tunnel.Signal` to use any signaling mechanism:

```go
type Signal interface {
    SendSignal(detail *NATDetail) error
    ReadSignal() (*NATDetail, error)
}
```

Built-in implementations in `example/signal.go`:
- `MockSignal` — stdin/stdout, for local testing
- `WebsocketSignal` — WebSocket-based signal server
- `CloudflareSignal` — Cloudflare Worker + KV

## Reference

- [Birthday Problem](https://en.wikipedia.org/wiki/Birthday_problem)
- [How NAT Traversal Works](https://tailscale.com/blog/how-nat-traversal-works/)
