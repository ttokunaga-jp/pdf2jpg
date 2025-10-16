package auth

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"
)

var discardLogger = log.New(io.Discard, "", 0)

func TestKeyService_IssueAndValidate(t *testing.T) {
	repo := newMemoryRepository()
	clock := &stubClock{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	service := NewKeyService(repo, discardLogger, nil, ServiceConfig{Clock: clock})

	resp, err := service.IssueTemporaryKey(context.Background(), IssueRequest{
		Label:      "test",
		UsageLimit: 1,
		TTL:        time.Hour,
		Operator:   "operator",
	})
	if err != nil {
		t.Fatalf("IssueTemporaryKey() error = %v", err)
	}
	if len(resp.Key) != defaultKeyBytes {
		t.Fatalf("expected key length %d, got %d", defaultKeyBytes, len(resp.Key))
	}

	record, outcome, err := service.ValidateAndConsume(context.Background(), resp.Key)
	if err != nil {
		t.Fatalf("ValidateAndConsume() error = %v", err)
	}
	if outcome != validationOutcomeAuthorized {
		t.Fatalf("expected outcome authorized, got %s", outcome)
	}
	if record.RemainingUsage != 0 {
		t.Fatalf("expected remaining usage 0, got %d", record.RemainingUsage)
	}

	_, outcome, err = service.ValidateAndConsume(context.Background(), resp.Key)
	if err == nil {
		t.Fatal("expected error after second consume")
	}
	if !errors.Is(err, ErrKeyExhausted) {
		t.Fatalf("expected ErrKeyExhausted, got %v", err)
	}
	if outcome != validationOutcomeExhausted {
		t.Fatalf("expected outcome exhausted, got %s", outcome)
	}
}

func TestKeyService_ValidateCache(t *testing.T) {
	repo := newMemoryRepository()
	service := NewKeyService(repo, discardLogger, nil, ServiceConfig{})

	_, outcome, err := service.ValidateAndConsume(context.Background(), "missing")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
	if outcome != validationOutcomeUnauthorized {
		t.Fatalf("expected unauthorized outcome, got %s", outcome)
	}

	_, outcome, err = service.ValidateAndConsume(context.Background(), "missing")
	if repo.consumeCalls["missing"] != 1 {
		t.Fatalf("expected single repository call, got %d", repo.consumeCalls["missing"])
	}
	if outcome != validationOutcomeUnauthorized {
		t.Fatalf("expected cached unauthorized outcome, got %s", outcome)
	}
}

func TestKeyService_Revoke(t *testing.T) {
	repo := newMemoryRepository()
	clock := &stubClock{now: time.Now().UTC()}
	service := NewKeyService(repo, discardLogger, nil, ServiceConfig{Clock: clock})

	resp, err := service.IssueTemporaryKey(context.Background(), IssueRequest{
		Label:      "trial",
		UsageLimit: 5,
		TTL:        time.Hour,
		Operator:   "operator",
	})
	if err != nil {
		t.Fatalf("IssueTemporaryKey() error = %v", err)
	}

	record, err := service.Revoke(context.Background(), resp.Key, "operator")
	if err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if record.RevokedAt == nil {
		t.Fatal("expected revoked timestamp to be set")
	}

	_, outcome, err := service.ValidateAndConsume(context.Background(), resp.Key)
	if !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("expected ErrKeyRevoked, got %v", err)
	}
	if outcome != validationOutcomeRevoked {
		t.Fatalf("expected revoked outcome, got %s", outcome)
	}
}

func TestKeyService_CleanupExpired(t *testing.T) {
	repo := newMemoryRepository()
	clock := &stubClock{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	service := NewKeyService(repo, discardLogger, nil, ServiceConfig{Clock: clock})

	if _, err := service.IssueTemporaryKey(context.Background(), IssueRequest{
		Label:      "expired",
		UsageLimit: 1,
		TTL:        time.Minute,
		Operator:   "op",
	}); err != nil {
		t.Fatalf("failed to issue expired key: %v", err)
	}

	if _, err := service.IssueTemporaryKey(context.Background(), IssueRequest{
		Label:      "active",
		UsageLimit: 1,
		TTL:        24 * time.Hour,
		Operator:   "op",
	}); err != nil {
		t.Fatalf("failed to issue active key: %v", err)
	}

	clock.Step(2 * time.Minute)
	deleted, err := service.CleanupExpired(context.Background(), 10)
	if err != nil {
		t.Fatalf("CleanupExpired error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected deleted 1, got %d", deleted)
	}
}

type memoryRepository struct {
	mu           sync.Mutex
	data         map[string]APIKey
	consumeCalls map[string]int
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		data:         make(map[string]APIKey),
		consumeCalls: make(map[string]int),
	}
}

func (m *memoryRepository) CreateTemporaryKey(ctx context.Context, key APIKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[key.Key]; exists {
		return errors.New("duplicate key")
	}
	m.data[key.Key] = key
	return nil
}

func (m *memoryRepository) Get(ctx context.Context, key string) (APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.data[key]
	if !ok {
		return APIKey{}, ErrKeyNotFound
	}
	return record, nil
}

func (m *memoryRepository) Consume(ctx context.Context, key string, now time.Time) (APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consumeCalls[key]++
	record, ok := m.data[key]
	if !ok {
		return APIKey{}, ErrKeyNotFound
	}
	switch {
	case record.RevokedAt != nil:
		return APIKey{}, ErrKeyRevoked
	case record.IsExpired(now):
		return APIKey{}, ErrKeyExpired
	case record.RemainingUsage <= 0:
		return APIKey{}, ErrKeyExhausted
	}
	record.RemainingUsage--
	m.data[key] = record
	return record, nil
}

func (m *memoryRepository) Revoke(ctx context.Context, key string, now time.Time) (APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.data[key]
	if !ok {
		return APIKey{}, ErrKeyNotFound
	}
	record.RemainingUsage = 0
	record.RevokedAt = &now
	m.data[key] = record
	return record, nil
}

func (m *memoryRepository) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *memoryRepository) DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	deleted := 0
	for k, v := range m.data {
		if deleted >= limit {
			break
		}
		if v.IsExpired(now) {
			delete(m.data, k)
			deleted++
		}
	}
	return deleted, nil
}

func (m *memoryRepository) CountActive(ctx context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, v := range m.data {
		if v.RevokedAt == nil && !v.IsExpired(now) && v.RemainingUsage > 0 {
			count++
		}
	}
	return count, nil
}

type stubClock struct {
	now time.Time
}

func (s *stubClock) Now() time.Time {
	return s.now
}

func (s *stubClock) Step(d time.Duration) {
	s.now = s.now.Add(d)
}
