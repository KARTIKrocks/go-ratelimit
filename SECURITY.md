# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.x.x   | :white_check_mark: |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them via [GitHub Security Advisories](https://github.com/KARTIKrocks/go-ratelimit/security/advisories) on this repository.

You should receive a response within 48 hours.

Please include:

- Type of vulnerability
- Full paths of source file(s) related to the vulnerability
- Location of the affected source code (tag/branch/commit or direct URL)
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the issue

## Security Considerations

### Input Validation

#### Key Validation
- Keys should be validated before use
- Avoid keys that could cause hash collisions
- Consider key length limits
- Sanitize user-provided keys

```go
// Example: Validate key length
func validateKey(key string) error {
    if len(key) > 256 {
        return errors.New("key too long")
    }
    if len(key) == 0 {
        return errors.New("key cannot be empty")
    }
    return nil
}
```

#### Rate/Limit Validation
- Validate rate and limit parameters
- Prevent integer overflow
- Check for negative values
- Set reasonable upper bounds

### Denial of Service Protection

#### Memory Exhaustion

**Risk**: Unlimited key creation can exhaust memory

**Mitigation**:
- Use cleanup intervals to remove inactive keys
- Set maximum number of active keys with `SetMaxKeys`
- Monitor memory usage
- Use distributed storage for large-scale

```go
// Good: Cleanup removes inactive keys + cap tracked keys
limiter := ratelimit.NewKeyedTokenBucket(
    10.0,
    20,
    5*time.Minute, // Cleanup every 5 minutes
)
limiter.SetMaxKeys(10000) // Cap at 10,000 tracked keys
defer limiter.Close()
```

#### CPU Exhaustion

**Risk**: Excessive rate limit checks can consume CPU

**Mitigation**:
- Efficient algorithms with O(1) operations
- Use Redis for distributed scenarios
- Consider sampling for very high traffic
- Implement circuit breakers

#### Slowloris Attacks

**Risk**: Slow requests can exhaust resources

**Mitigation**:
- Use WaitMiddleware with timeouts
- Configure HTTP server timeouts
- Monitor connection durations

```go
handler := ratelimit.WaitMiddleware(
    limiter,
    30*time.Second, // Maximum wait time
)(yourHandler)
```

### Redis Security

#### Connection Security

```go
// Use TLS for production
client := redis.NewClient(&redis.Options{
    Addr:      "redis:6379",
    Password:  os.Getenv("REDIS_PASSWORD"),
    TLSConfig: &tls.Config{
        MinVersion: tls.VersionTLS12,
    },
})
```

#### Authentication

- Always use authentication in production
- Use strong passwords
- Rotate credentials regularly
- Use environment variables for secrets

```go
// Good: Use environment variables
redisPassword := os.Getenv("REDIS_PASSWORD")

// Bad: Hardcoded credentials
redisPassword := "password123" // ❌ Never do this
```

#### Network Security

- Use private networks for Redis
- Restrict Redis port access
- Use firewalls
- Consider Redis ACLs

### Rate Limit Bypass Prevention

#### IP Spoofing

**Risk**: Attackers may spoof X-Forwarded-For headers

**Mitigation**:

By default, `GetClientIP` uses only `RemoteAddr` (not spoofable). To trust
proxy headers, use `GetClientIPFromHeaders` or `TrustedProxyKeyFunc` explicitly:

```go
// Default: safe, uses RemoteAddr only
ratelimit.WithKeyFunc(ratelimit.IPKeyFunc)

// Opt-in: trust proxy headers (X-Forwarded-For, X-Real-IP, CF-Connecting-IP)
ratelimit.WithKeyFunc(ratelimit.TrustedProxyKeyFunc)
```

#### Distributed Attacks

**Risk**: Distributed attacks from multiple IPs

**Mitigation**:
- Implement global rate limits
- Use composite limiters
- Monitor traffic patterns
- Use CDN/WAF

```go
// Global + per-IP limits
globalLimit := ratelimit.NewTokenBucket(10000.0, 20000)
perIPLimit := ratelimit.NewKeyedTokenBucket(100.0, 200, time.Minute)

multi := ratelimit.NewMulti(
    globalLimit,
    // Per-IP check via middleware
)
```

#### Key Enumeration

**Risk**: Attackers may enumerate valid keys

**Mitigation**:
- Use consistent response times
- Don't leak information in error messages
- Use rate limiting on authentication endpoints
- Implement account lockout

### Information Disclosure

#### Headers

**Risk**: Headers may reveal system information

**Mitigation**:
```go
// Disable headers in production if needed
ratelimit.WithHeaders(false)
```

#### Error Messages

**Risk**: Detailed errors may leak system information

**Mitigation**:
```go
// Use generic error messages
func safeOnLimitReached(w http.ResponseWriter, r *http.Request, result ratelimit.Result) {
    // Don't reveal internal details
    http.Error(w, "Too many requests", http.StatusTooManyRequests)
}
```

### Timing Attacks

**Risk**: Response time differences may reveal information

**Mitigation**:
- Use constant-time comparisons for sensitive operations
- Add random delays if necessary
- Don't leak information via timing

## Best Practices

### Production Deployment

1. **Use HTTPS**: Always use TLS in production
2. **Environment Variables**: Store credentials in env vars
3. **Monitoring**: Set up alerts for unusual patterns
4. **Logging**: Log rate limit violations (but not sensitive data)
5. **Regular Updates**: Keep dependencies updated
6. **Backup**: Have fallback mechanisms

### Configuration

```go
// Production-ready configuration
limiter := ratelimit.NewKeyedTokenBucket(
    100.0/60.0,     // Reasonable rate
    20,              // Moderate burst
    5*time.Minute,   // Regular cleanup
)

handler := ratelimit.Middleware(limiter,
    ratelimit.WithKeyFunc(ratelimit.IPKeyFunc),
    ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
    ratelimit.WithSkipFunc(ratelimit.SkipHealthChecks),
)(yourHandler)
```

### Monitoring

Monitor for:
- High rate limit hit rates
- Unusual traffic patterns
- Memory usage growth
- Redis connection issues
- Error rates

### Testing

- Test with malicious inputs
- Fuzz test key handling
- Load test with realistic traffic
- Test failure scenarios
- Verify cleanup works correctly

## Vulnerability Disclosure Timeline

1. **T+0**: Vulnerability reported
2. **T+48h**: Initial response
3. **T+7d**: Vulnerability confirmed/rejected
4. **T+30d**: Fix developed and tested
5. **T+60d**: Fix released
6. **T+90d**: Public disclosure (if fixed)

## Security Checklist

- [ ] Rate and limit parameters validated
- [ ] Keys validated and sanitized
- [ ] Cleanup intervals configured
- [ ] Redis authentication enabled
- [ ] TLS configured for Redis
- [ ] Secrets in environment variables
- [ ] Monitoring and alerting set up
- [ ] Error messages don't leak info
- [ ] HTTP timeouts configured
- [ ] Regular security updates

## Contact

For security issues: [GitHub Security Advisories](https://github.com/KARTIKrocks/go-ratelimit/security/advisories)
For general questions: [GitHub Issues](https://github.com/KARTIKrocks/go-ratelimit/issues)

## Acknowledgments

We thank the security researchers who have helped improve this package.