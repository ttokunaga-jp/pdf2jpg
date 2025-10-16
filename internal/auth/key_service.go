package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"time"
)

const (
	defaultKeyBytes          = 32
	negativeCacheTTL         = 30 * time.Second
	errorCacheTTL            = 5 * time.Second
	defaultCleanupLimit      = 200
	operatorHashPrefixLength = 16
	apiKeyHashPrefixLength   = 16
)

type clock interface {
	Now() time.Time
}

type timeNowClock struct{}

func (timeNowClock) Now() time.Time {
	return time.Now().UTC()
}

// KeyService coordinates issuance, validation and lifecycle operations for temporary API keys.
type KeyService struct {
	repo    Repository
	logger  *log.Logger
	metrics metricsRecorder
	clock   clock
	cache   *decisionCache
}

// ServiceConfig captures optional tunables for KeyService behaviour.
type ServiceConfig struct {
	Clock clock
}

func NewKeyService(repo Repository, logger *log.Logger, metrics metricsRecorder, cfg ServiceConfig) *KeyService {
	clk := cfg.Clock
	if clk == nil {
		clk = timeNowClock{}
	}
	if metrics == nil {
		metrics = newExpvarMetrics()
	}
	return &KeyService{
		repo:    repo,
		logger:  logger,
		metrics: metrics,
		clock:   clk,
		cache:   newDecisionCache(),
	}
}

type IssueRequest struct {
	Label      string
	UsageLimit int
	TTL        time.Duration
	Operator   string
}

type IssueResponse struct {
	Key    string
	Record APIKey
}

func (s *KeyService) IssueTemporaryKey(ctx context.Context, req IssueRequest) (IssueResponse, error) {
	rawKey, err := generateBase62Key(defaultKeyBytes)
	operatorHash := hashIdentifier(req.Operator, operatorHashPrefixLength)
	if err != nil {
		s.metrics.IncKeyIssue("error", operatorHash)
		return IssueResponse{}, fmt.Errorf("generate api key: %w", err)
	}

	now := s.clock.Now()
	record := APIKey{
		Key:            rawKey,
		Type:           TemporaryKey,
		Label:          req.Label,
		CreatedAt:      now,
		ExpiresAt:      now.Add(req.TTL),
		MaxUsage:       req.UsageLimit,
		RemainingUsage: req.UsageLimit,
	}

	if err := s.repo.CreateTemporaryKey(ctx, record); err != nil {
		s.metrics.IncKeyIssue("error", operatorHash)
		return IssueResponse{}, fmt.Errorf("persist api key: %w", err)
	}

	s.cache.Delete(rawKey)
	s.metrics.IncKeyIssue("success", operatorHash)
	if err := s.refreshActiveGauge(ctx); err != nil {
		s.logger.Printf("WARN: refresh active keys gauge: %v", err)
	}

	keyHash := hashIdentifier(rawKey, apiKeyHashPrefixLength)
	s.logger.Printf("INFO: event=api_key_issue api_key_hash=%s operator=%s label=%q usage_limit=%d ttl=%s", keyHash, operatorHash, req.Label, req.UsageLimit, req.TTL)
	return IssueResponse{Key: rawKey, Record: record}, nil
}

func (s *KeyService) Get(ctx context.Context, key string) (APIKey, error) {
	record, err := s.repo.Get(ctx, key)
	if err != nil {
		return APIKey{}, err
	}
	return record, nil
}

func (s *KeyService) Revoke(ctx context.Context, key string, operator string) (APIKey, error) {
	record, err := s.repo.Revoke(ctx, key, s.clock.Now())
	if err != nil {
		return APIKey{}, err
	}
	s.cache.Set(key, validationOutcomeRevoked, negativeCacheTTL, s.clock.Now())
	if err := s.refreshActiveGauge(ctx); err != nil {
		s.logger.Printf("WARN: refresh active keys gauge: %v", err)
	}
	s.logger.Printf("INFO: event=api_key_revoke api_key_hash=%s operator=%s", hashIdentifier(key, apiKeyHashPrefixLength), hashIdentifier(operator, operatorHashPrefixLength))
	return record, nil
}

func (s *KeyService) CleanupExpired(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = defaultCleanupLimit
	}

	count, err := s.repo.DeleteExpired(ctx, s.clock.Now(), limit)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		if err := s.refreshActiveGauge(ctx); err != nil {
			s.logger.Printf("WARN: refresh active keys gauge: %v", err)
		}
	}
	return count, nil
}

func (s *KeyService) ValidateAndConsume(ctx context.Context, key string) (APIKey, validationOutcome, error) {
	if outcome, ok := s.cache.Get(key, s.clock.Now()); ok {
		if outcome == validationOutcomeAuthorized {
			// We never cache positive decisions.
			return APIKey{}, outcome, nil
		}
		s.metrics.IncKeyValidation(outcome)
		return APIKey{}, outcome, outcome.errEquivalent()
	}

	record, err := s.repo.Consume(ctx, key, s.clock.Now())
	if err == nil {
		s.cache.Delete(key)
		s.metrics.IncKeyValidation(validationOutcomeAuthorized)
		return record, validationOutcomeAuthorized, nil
	}

	outcome := mapErrorToOutcome(err)
	ttl := negativeCacheTTL
	if outcome == validationOutcomeError {
		ttl = errorCacheTTL
	}
	s.cache.Set(key, outcome, ttl, s.clock.Now())
	if errors.Is(err, ErrKeyExpired) {
		if delErr := s.repo.Delete(ctx, key); delErr != nil {
			s.logger.Printf("WARN: delete expired key: %v", delErr)
		} else if err := s.refreshActiveGauge(ctx); err != nil {
			s.logger.Printf("WARN: refresh active keys gauge: %v", err)
		}
	}
	s.metrics.IncKeyValidation(outcome)
	return APIKey{}, outcome, err
}

// DefaultCleanupLimit returns the default maximum number of documents deleted per cleanup run.
func DefaultCleanupLimit() int {
	return defaultCleanupLimit
}

func (s *KeyService) refreshActiveGauge(ctx context.Context) error {
	count, err := s.repo.CountActive(ctx, s.clock.Now())
	if err != nil {
		return err
	}
	s.metrics.SetTemporaryKeysActive(count)
	return nil
}

func mapErrorToOutcome(err error) validationOutcome {
	switch {
	case errors.Is(err, ErrKeyNotFound):
		return validationOutcomeUnauthorized
	case errors.Is(err, ErrKeyExpired):
		return validationOutcomeExpired
	case errors.Is(err, ErrKeyRevoked):
		return validationOutcomeRevoked
	case errors.Is(err, ErrKeyExhausted):
		return validationOutcomeExhausted
	default:
		return validationOutcomeError
	}
}

func (o validationOutcome) errEquivalent() error {
	switch o {
	case validationOutcomeUnauthorized:
		return ErrKeyNotFound
	case validationOutcomeExpired:
		return ErrKeyExpired
	case validationOutcomeRevoked:
		return ErrKeyRevoked
	case validationOutcomeExhausted:
		return ErrKeyExhausted
	default:
		return errors.New("validation error")
	}
}

func generateBase62Key(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("invalid key length")
	}

	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	const charsetLen = byte(len(charset))
	const maxMultiple = 256 / int(charsetLen) * int(charsetLen)

	out := make([]byte, length)
	buffer := make([]byte, length)
	// Rejection sample random bytes to avoid modulo bias.
	for i := 0; i < length; {
		if _, err := rand.Read(buffer); err != nil {
			return "", fmt.Errorf("read random bytes: %w", err)
		}
		for _, b := range buffer {
			if int(b) >= maxMultiple {
				continue
			}
			idx := b % charsetLen
			out[i] = charset[idx]
			i++
			if i == length {
				break
			}
		}
	}
	return string(out), nil
}

func hashIdentifier(value string, prefix int) string {
	sum := sha256.Sum256([]byte(value))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	if prefix > 0 && prefix < len(encoded) {
		return encoded[:prefix]
	}
	return encoded
}
