package auth

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrKeyNotFound indicates that the requested key does not exist.
	ErrKeyNotFound = errors.New("api key not found")
	// ErrKeyExpired indicates that the key has surpassed its expiration timestamp.
	ErrKeyExpired = errors.New("api key expired")
	// ErrKeyRevoked indicates an operator revoked the key.
	ErrKeyRevoked = errors.New("api key revoked")
	// ErrKeyExhausted indicates the remaining usage count reached zero.
	ErrKeyExhausted = errors.New("api key usage exhausted")
)

// Repository abstracts the storage operations required for temporary API keys.
type Repository interface {
	CreateTemporaryKey(ctx context.Context, key APIKey) error
	Get(ctx context.Context, key string) (APIKey, error)
	Consume(ctx context.Context, key string, now time.Time) (APIKey, error)
	Revoke(ctx context.Context, key string, now time.Time) (APIKey, error)
	Delete(ctx context.Context, key string) error
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error)
	CountActive(ctx context.Context, now time.Time) (int, error)
}
