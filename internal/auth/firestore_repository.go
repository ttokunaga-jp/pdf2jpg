package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultCollection = "apiKeys"
	maxRetries        = 3
	requestTimeout    = 3 * time.Second
	initialBackoff    = 100 * time.Millisecond
)

type FirestoreRepository struct {
	client     *firestore.Client
	collection string
	tracer     trace.Tracer
}

func NewFirestoreRepository(client *firestore.Client, collection string) *FirestoreRepository {
	if collection == "" {
		collection = defaultCollection
	}
	return &FirestoreRepository{
		client:     client,
		collection: collection,
		tracer:     otel.Tracer("pdf2jpg/internal/auth/firestore"),
	}
}

func (r *FirestoreRepository) CreateTemporaryKey(ctx context.Context, key APIKey) error {
	return r.withRetries(ctx, "CreateTemporaryKey", func(ctx context.Context) error {
		doc := r.collectionRef().Doc(key.Key)
		_, err := doc.Create(ctx, encodeAPIKey(key))
		return err
	})
}

func (r *FirestoreRepository) Get(ctx context.Context, key string) (APIKey, error) {
	var result APIKey
	err := r.withRetries(ctx, "GetTemporaryKey", func(ctx context.Context) error {
		doc, err := r.collectionRef().Doc(key).Get(ctx)
		if status.Code(err) == codes.NotFound {
			return ErrKeyNotFound
		}
		if err != nil {
			return err
		}
		result, err = decodeAPIDocument(doc)
		return err
	})
	return result, err
}

func (r *FirestoreRepository) Consume(ctx context.Context, key string, now time.Time) (APIKey, error) {
	var result APIKey
	err := r.withRetries(ctx, "ConsumeTemporaryKey", func(ctx context.Context) error {
		doc := r.collectionRef().Doc(key)
		err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			snap, err := tx.Get(doc)
			if status.Code(err) == codes.NotFound {
				return ErrKeyNotFound
			}
			if err != nil {
				return err
			}

			record, err := decodeAPIDocument(snap)
			if err != nil {
				return err
			}
			switch {
			case record.RevokedAt != nil:
				return ErrKeyRevoked
			case record.IsExpired(now):
				return ErrKeyExpired
			case record.RemainingUsage <= 0:
				return ErrKeyExhausted
			}

			record.RemainingUsage--
			if err := tx.Set(doc, encodeAPIKey(record)); err != nil {
				return err
			}
			result = record
			return nil
		}, firestore.MaxAttempts(1))

		return err
	})
	return result, err
}

func (r *FirestoreRepository) Revoke(ctx context.Context, key string, now time.Time) (APIKey, error) {
	var result APIKey
	err := r.withRetries(ctx, "RevokeTemporaryKey", func(ctx context.Context) error {
		doc := r.collectionRef().Doc(key)
		err := r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			snap, err := tx.Get(doc)
			if status.Code(err) == codes.NotFound {
				return ErrKeyNotFound
			}
			if err != nil {
				return err
			}
			record, err := decodeAPIDocument(snap)
			if err != nil {
				return err
			}
			if record.RevokedAt != nil {
				result = record
				return nil
			}
			record.RemainingUsage = 0
			record.RevokedAt = &now
			if err := tx.Set(doc, encodeAPIKey(record)); err != nil {
				return err
			}
			result = record
			return nil
		}, firestore.MaxAttempts(1))
		return err
	})
	return result, err
}

func (r *FirestoreRepository) Delete(ctx context.Context, key string) error {
	return r.withRetries(ctx, "DeleteTemporaryKey", func(ctx context.Context) error {
		_, err := r.collectionRef().Doc(key).Delete(ctx)
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	})
}

func (r *FirestoreRepository) DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	var deleted int
	err := r.withRetries(ctx, "DeleteExpiredKeys", func(ctx context.Context) error {
		q := r.collectionRef().Where("expires_at", "<=", now).Limit(limit)
		iter := q.Documents(ctx)
		defer iter.Stop()

		batch := r.client.Batch()
		for {
			doc, err := iter.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				return err
			}
			batch.Delete(doc.Ref)
			deleted++
		}
		if deleted == 0 {
			return nil
		}
		_, err := batch.Commit(ctx)
		return err
	})
	return deleted, err
}

func (r *FirestoreRepository) CountActive(ctx context.Context, now time.Time) (int, error) {
	var count int
	err := r.withRetries(ctx, "CountActiveKeys", func(ctx context.Context) error {
		iter := r.collectionRef().
			Where("remaining_usage", ">", 0).
			Where("expires_at", ">", now).
			Documents(ctx)
		defer iter.Stop()
		for {
			_, err := iter.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func (r *FirestoreRepository) collectionRef() *firestore.CollectionRef {
	return r.client.Collection(r.collection)
}

func (r *FirestoreRepository) withRetries(ctx context.Context, spanName string, fn func(context.Context) error) error {
	var err error
	backoff := initialBackoff
	for attempt := 0; attempt < maxRetries; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		spanCtx, span := r.tracer.Start(attemptCtx, spanName)
		err = fn(spanCtx)
		span.End()
		cancel()
		if err == nil || isNonRetryableError(err) || attempt == maxRetries-1 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return err
}

func decodeAPIDocument(doc *firestore.DocumentSnapshot) (APIKey, error) {
	var payload struct {
		Type           string     `firestore:"type"`
		Label          string     `firestore:"label"`
		CreatedAt      time.Time  `firestore:"created_at"`
		ExpiresAt      time.Time  `firestore:"expires_at"`
		MaxUsage       int        `firestore:"max_usage"`
		RemainingUsage int        `firestore:"remaining_usage"`
		RevokedAt      *time.Time `firestore:"revoked_at"`
	}
	if err := doc.DataTo(&payload); err != nil {
		return APIKey{}, fmt.Errorf("decode api key document: %w", err)
	}
	record := APIKey{
		Key:            doc.Ref.ID,
		Type:           KeyType(payload.Type),
		Label:          payload.Label,
		CreatedAt:      payload.CreatedAt,
		ExpiresAt:      payload.ExpiresAt,
		MaxUsage:       payload.MaxUsage,
		RemainingUsage: payload.RemainingUsage,
		RevokedAt:      payload.RevokedAt,
	}
	return record, nil
}

func isNonRetryableError(err error) bool {
	switch status.Code(err) {
	case codes.OK:
		return true
	case codes.NotFound, codes.InvalidArgument, codes.FailedPrecondition, codes.PermissionDenied, codes.AlreadyExists:
		return true
	default:
		return errors.Is(err, ErrKeyNotFound) ||
			errors.Is(err, ErrKeyExpired) ||
			errors.Is(err, ErrKeyRevoked) ||
			errors.Is(err, ErrKeyExhausted)
	}
}

func encodeAPIKey(record APIKey) map[string]interface{} {
	data := map[string]interface{}{
		"type":            string(record.Type),
		"label":           record.Label,
		"created_at":      record.CreatedAt,
		"expires_at":      record.ExpiresAt,
		"max_usage":       record.MaxUsage,
		"remaining_usage": record.RemainingUsage,
	}
	if record.RevokedAt != nil {
		data["revoked_at"] = *record.RevokedAt
	}
	return data
}
