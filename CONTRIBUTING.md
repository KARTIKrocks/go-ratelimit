# Contributing to ratelimit

Thank you for your interest in contributing to ratelimit! This document provides guidelines and instructions for contributing.

## Code of Conduct

This project adheres to a code of conduct. By participating, you are expected to uphold this code. Please be respectful and constructive in all interactions.

## How to Contribute

### Reporting Bugs

Before creating bug reports, please check the issue tracker to avoid duplicates. When creating a bug report, include:

- **Clear title and description**
- **Steps to reproduce** the issue
- **Expected behavior** vs **actual behavior**
- **Go version** and **OS**
- **Code samples** demonstrating the issue
- **Stack traces** if applicable

### Suggesting Enhancements

Enhancement suggestions are tracked as GitHub issues. When creating an enhancement suggestion, include:

- **Clear title and description**
- **Use case** and **rationale**
- **Expected API** or behavior
- **Examples** of how it would be used

### Pull Requests

1. **Fork the repository** and create your branch from `main`
2. **Write tests** for any new functionality
3. **Ensure all tests pass** with `go test ./...`
4. **Run the race detector** with `go test -race ./...`
5. **Update documentation** as needed
6. **Follow Go conventions** and run `gofmt`
7. **Write a clear commit message**

#### Pull Request Process

1. Update README.md with details of changes if needed
2. Add tests covering your changes
3. Ensure CI passes
4. Request review from maintainers
5. Address review feedback
6. Squash commits before merge

## Development Setup

```bash
# Clone your fork
git clone https://github.com/KARTIKrocks/go-ratelimit.git
cd ratelimit

# Install dependencies
go mod download

# Run tests
go test ./...

# Run with race detector
go test -race ./...

# Run benchmarks
go test -bench=. -benchmem ./...
```

## Testing

### Writing Tests

- Write table-driven tests where appropriate
- Test edge cases and error conditions
- Include benchmarks for performance-critical code
- Use meaningful test names that describe what is being tested

Example:

```go
func TestTokenBucket_Allow(t *testing.T) {
    tests := []struct {
        name     string
        rate     float64
        burst    int
        requests int
        want     int
    }{
        {"basic", 10.0, 10, 5, 5},
        {"burst", 10.0, 10, 15, 10},
        {"zero rate", 0, 10, 5, 0},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            limiter := NewTokenBucket(tt.rate, tt.burst)
            // ... test logic
        })
    }
}
```

### Running Tests

```bash
# All tests
go test ./...

# With coverage
go test -cover ./...

# Specific package
go test ./metrics

# Verbose output
go test -v ./...

# Race detector
go test -race ./...
```

## Code Style

### General Guidelines

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` for formatting
- Run `go vet` to catch common mistakes
- Keep functions small and focused
- Document exported types and functions
- Use meaningful variable names

### Documentation

- Document all exported types, functions, and methods
- Include examples for complex functionality
- Keep comments concise and informative
- Use complete sentences

Example:

```go
// TokenBucket implements the token bucket algorithm.
// It allows bursts up to the bucket capacity while maintaining
// a steady rate over time.
//
// Example:
//   limiter := NewTokenBucket(10.0, 20)
//   if limiter.Allow() {
//       // process request
//   }
type TokenBucket struct {
    // ...
}
```

### Error Handling

- Return errors rather than panicking
- Wrap errors with context using `fmt.Errorf`
- Use sentinel errors for expected conditions
- Document error return values

### Concurrency

- Use mutexes to protect shared state
- Prefer channels for communication
- Document thread-safety guarantees
- Test for race conditions

## Performance

- Write benchmarks for new features
- Avoid allocations in hot paths
- Use sync.Pool for frequently allocated objects
- Profile code to identify bottlenecks

Example benchmark:

```go
func BenchmarkTokenBucket_Allow(b *testing.B) {
    limiter := NewTokenBucket(1000000.0, 1000000)
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        limiter.Allow()
    }
}
```

## Commit Messages

- Use the present tense ("Add feature" not "Added feature")
- Use the imperative mood ("Move cursor to..." not "Moves cursor to...")
- Limit the first line to 72 characters
- Reference issues and pull requests

Example:

```
Add sliding window counter algorithm

Implements sliding window counter for more accurate rate limiting
without the boundary issues of fixed windows.

Fixes #123
```

## Release Process

1. Update version in documentation
2. Update CHANGELOG.md
3. Create and push tag
4. GitHub Actions will create release
5. Announce in discussions

## Questions?

- **Issues**: For bugs and feature requests
- **Discussions**: For questions and general discussion
- **Security**: Via GitHub Security Advisories on the repository

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
