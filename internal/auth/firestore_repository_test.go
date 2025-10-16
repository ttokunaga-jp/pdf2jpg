package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

func TestFirestoreRepositoryLifecycle(t *testing.T) {
	emulator := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulator == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set; skipping Firestore integration test")
	}

	projectID := os.Getenv("FIRESTORE_PROJECT_ID")
	if projectID == "" {
		projectID = "test-project"
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		t.Fatalf("firestore.NewClient: %v", err)
	}
	defer client.Close()

	repo := NewFirestoreRepository(client, "integrationKeys")

	keyValue, err := generateBase62Key(16)
	if err != nil {
		t.Fatalf("generateBase62Key: %v", err)
	}
	now := time.Now().UTC()
	record := APIKey{
		Key:            keyValue,
		Type:           TemporaryKey,
		Label:          "integration",
		CreatedAt:      now,
		ExpiresAt:      now.Add(time.Hour),
		MaxUsage:       1,
		RemainingUsage: 1,
	}

	if err := repo.CreateTemporaryKey(ctx, record); err != nil {
		t.Fatalf("CreateTemporaryKey: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Delete(context.Background(), keyValue)
	})

	stored, err := repo.Get(ctx, keyValue)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Key != keyValue {
		t.Fatalf("expected key %s, got %s", keyValue, stored.Key)
	}

	consumed, err := repo.Consume(ctx, keyValue, now)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if consumed.RemainingUsage != 0 {
		t.Fatalf("expected remaining usage 0, got %d", consumed.RemainingUsage)
	}

	if _, err := repo.Consume(ctx, keyValue, now); err == nil {
		t.Fatal("expected second consume to fail")
	}

	expiredKey := APIKey{
		Key:            keyValue + "-expired",
		Type:           TemporaryKey,
		Label:          "expired",
		CreatedAt:      now.Add(-2 * time.Hour),
		ExpiresAt:      now.Add(-time.Hour),
		MaxUsage:       1,
		RemainingUsage: 1,
	}
	if err := repo.CreateTemporaryKey(ctx, expiredKey); err != nil {
		t.Fatalf("CreateTemporaryKey expired: %v", err)
	}

	deleted, err := repo.DeleteExpired(ctx, now, 10)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if deleted == 0 {
		t.Fatal("expected expired key to be deleted")
	}
}
