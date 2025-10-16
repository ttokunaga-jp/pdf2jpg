package auth

import "time"

// KeyType identifies the origin of the API key. Currently only temporary keys are persisted.
type KeyType string

const (
	// TemporaryKey represents a Firestore backed, limited usage key.
	TemporaryKey KeyType = "temporary"
)

// APIKeyStatus is the lifecycle state of a key as exposed to operators.
type APIKeyStatus string

const (
	StatusActive    APIKeyStatus = "active"
	StatusExpired   APIKeyStatus = "expired"
	StatusExhausted APIKeyStatus = "exhausted"
	StatusRevoked   APIKeyStatus = "revoked"
)

// APIKey models the Firestore document persisted for temporary keys.
type APIKey struct {
	Key            string
	Type           KeyType
	Label          string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	MaxUsage       int
	RemainingUsage int
	RevokedAt      *time.Time
}

// Status returns the derived lifecycle status for the key at the provided time.
func (k APIKey) Status(now time.Time) APIKeyStatus {
	if k.RevokedAt != nil {
		return StatusRevoked
	}
	if now.After(k.ExpiresAt) {
		return StatusExpired
	}
	if k.RemainingUsage <= 0 {
		return StatusExhausted
	}
	return StatusActive
}

// IsExpired returns true when the key should be considered expired.
func (k APIKey) IsExpired(now time.Time) bool {
	return now.After(k.ExpiresAt)
}
