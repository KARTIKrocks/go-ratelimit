---
id: key-functions
title: Key Functions
description: How the middleware turns a request into a rate limit key — client IP, headers, user IDs, paths — and how to get the real client IP safely behind reverse proxies.
---

# Key Functions

A `KeyFunc` maps a request to the key it is limited under:

```go
type KeyFunc func(r *http.Request) string
```

Requests with the same key share a limit. Choosing the key is choosing _who_
the limit applies to.

## Built-in key functions

| Key function | Key |
| --- | --- |
| `IPKeyFunc` | client IP from `RemoteAddr` — the default |
| `TrustedProxyKeyFunc` | client IP from proxy headers, behind exactly one proxy |
| `TrustedProxiesKeyFunc(cidrs...)` | client IP behind one or more listed proxies |
| `HeaderKeyFunc(name)` | a header value, such as an API key, falling back to the client IP |
| `UserIDKeyFunc(ctxKey)` | a user ID stored in the request context, falling back to the client IP |
| `PathKeyFunc` | the URL path — one shared limit per endpoint |
| `MethodPathKeyFunc` | method and path, e.g. `POST:/orders` |
| `IPPathKeyFunc` | client IP and path — a separate limit per client per endpoint |
| `CompositeKeyFunc(fns...)` | the results of several key functions joined with `:` |

## Client IP

`IPKeyFunc` uses `RemoteAddr`, the address of the peer that opened the TCP
connection. It cannot be spoofed by the client, which makes it the safe
default.

### IPv6 clients

Each IPv6 address is its own key. Many providers give one customer a whole
/64 block, so a client that controls one can rotate through addresses to get a
fresh limit. If that matters for your deployment, build a key function on
`GetClientIP` that masks IPv6 addresses to their /64 network — later releases
do this by default.

## Behind a reverse proxy

Behind a load balancer, CDN, or ingress controller, `RemoteAddr` is the proxy,
so every client shares one limit. The real client address is in a header the
proxy adds — but a client can send those headers too, so trusting them blindly
lets anyone pick their own key and dodge the limit.

### Exactly one proxy

`TrustedProxyKeyFunc` reads the client IP from, in order:

1. the **rightmost** `X-Forwarded-For` entry — the address your proxy saw,
   which the client cannot forge; entries to its left are client-supplied,
2. `X-Real-IP`,
3. `CF-Connecting-IP`,

and falls back to `RemoteAddr`.

```go
ratelimit.WithKeyFunc(ratelimit.TrustedProxyKeyFunc)
```

Use it only when **every** request reaches the server through exactly one proxy
that sets these headers. If clients can also reach the server directly, they
can set the headers themselves.

### Several proxies, or untrusted direct traffic

`TrustedProxiesKeyFunc` takes the networks your proxies live in, and only
trusts headers on requests that actually come from one of them:

```go
ratelimit.WithKeyFunc(ratelimit.TrustedProxiesKeyFunc(
    "10.0.0.0/8",     // internal load balancers
    "203.0.113.7",    // a single CDN edge — a bare IP means one address
))
```

- If `RemoteAddr` is not a trusted proxy, the key is `RemoteAddr`, and headers
  are ignored.
- Otherwise it walks `X-Forwarded-For` from the right, skipping trusted proxy
  hops, and uses the first address that is not a trusted proxy.

It panics at startup if a CIDR cannot be parsed. This is the right choice for
CDN-plus-load-balancer setups and any server that also accepts direct
connections.

### Reading the IP yourself

```go
ratelimit.GetClientIP(r)            // RemoteAddr only
ratelimit.GetClientIPFromHeaders(r) // the TrustedProxyKeyFunc rules
```

## API keys and users

```go
// One limit per API key
ratelimit.WithKeyFunc(ratelimit.HeaderKeyFunc("X-API-Key"))

// One limit per authenticated user
ratelimit.WithKeyFunc(ratelimit.UserIDKeyFunc(userIDKey))
```

`HeaderKeyFunc` uses the raw header value as the key. Requests without the
header all get the empty key and **share one limit**, so one unauthenticated
client can use it up for everyone. Reject requests without the header before
the limiter, or wrap the key function to fall back to the client IP:

```go
func apiKeyOrIP(r *http.Request) string {
    if key := r.Header.Get("X-API-Key"); key != "" {
        return "key:" + key
    }
    return "ip:" + ratelimit.IPKeyFunc(r)
}
```

`UserIDKeyFunc` formats the context value with `%v`. It must run after the
middleware that authenticates the request and stores the ID; put the rate
limiter inside your auth middleware, not outside it.

:::warning Limit before you authenticate, too

A limiter keyed on an API key only sees requests that carry one. Invented keys
each get a fresh limit, and each costs an entry in the limiter. Validate the
key first, or keep an IP-based limit in front of the key-based one.

:::

## Writing your own

Any function with the right signature works. Prefix keys with their kind so
different kinds cannot collide:

```go
func tenantKey(r *http.Request) string {
    if tenant := r.Header.Get("X-Tenant"); tenant != "" {
        return "tenant:" + tenant
    }
    return "ip:" + ratelimit.IPKeyFunc(r)
}
```
