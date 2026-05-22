package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/jackc/pgx/v5/pgconn"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/storage"
)

// decode decodes data into an object and sets its resource version.
func decode(codec runtime.Codec, data []byte, into runtime.Object, rv int64) error {
	_, _, err := codec.Decode(data, nil, into)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to decode object: %w", err))
	}
	versioner := storage.APIObjectVersioner{}
	if err := versioner.UpdateObject(into, uint64(rv)); err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to set resource version: %w", err))
	}
	return nil
}

// decodeToObject decodes data into a new runtime.Object of the type given by
// newFunc. Providing newFunc avoids internal-version conversions in the codec.
func decodeToObject(codec runtime.Codec, data []byte, newFunc func() runtime.Object) (runtime.Object, error) {
	into := newFunc()
	obj, _, err := codec.Decode(data, nil, into)
	if err != nil {
		return nil, fmt.Errorf("failed to decode object: %w", err)
	}
	return obj, nil
}

// policyRefJSON encodes a *corev1.ObjectReference as JSON for the
// source_policy_ref JSONB column. Returns nil so the column stores SQL NULL
// for manually-created relationships.
func policyRefJSON(ref *corev1.ObjectReference) []byte {
	if ref == nil {
		return nil
	}
	b, err := json.Marshal(ref)
	if err != nil {
		return nil
	}
	return b
}

// isUniqueViolation reports whether err is a Postgres unique_violation (23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	return strings.Contains(err.Error(), "duplicate key value violates unique constraint") ||
		strings.Contains(err.Error(), "23505")
}

// checkPreconditions validates storage preconditions against the existing object.
func checkPreconditions(key string, preconditions *storage.Preconditions, existing runtime.Object) error {
	if preconditions == nil {
		return nil
	}
	return preconditions.Check(key, existing)
}

// matchesPredicate checks if an object matches a storage selection predicate.
func matchesPredicate(obj runtime.Object, predicate storage.SelectionPredicate) (bool, error) {
	return predicate.Matches(obj)
}

// writeChangelog inserts a row into knowledge_changelog within the given transaction.
func writeChangelog(ctx context.Context, tx *sql.Tx, key string, rv int64, eventType string, data []byte) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO knowledge_changelog (key, resource_version, event_type, data, created_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		key, rv, eventType, data,
	)
	return err
}
