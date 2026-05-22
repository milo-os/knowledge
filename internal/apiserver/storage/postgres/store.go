package postgres

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // Postgres driver (pgx)
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/klog/v2"

	v1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

// Compile-time assertion that Store satisfies storage.Interface.
var _ storage.Interface = (*Store)(nil)

// Store implements storage.Interface for ResourceRelationship objects against
// PostgreSQL. Each Create/Update extracts typed columns from the spec so the
// BFS engine can issue index-friendly traversal queries, while the full
// object is persisted in object_data (JSONB) to satisfy the storage.Interface
// Get/List contract.
type Store struct {
	db        *sql.DB
	codec     runtime.Codec
	versioner storage.Versioner
	newFunc   func() runtime.Object
	watcher   *PostgresWatcher
	readyErr  atomic.Value
}

type readyErrHolder struct {
	err error
}

// NewStore creates a Postgres-backed storage.Interface for ResourceRelationships.
//
// dsn is used by the watcher to open a dedicated LISTEN/NOTIFY connection.
// When dsn is empty the watcher falls back to polling-only.
func NewStore(db *sql.DB, codec runtime.Codec, dsn string) *Store {
	return &Store{
		db:        db,
		codec:     codec,
		versioner: storage.APIObjectVersioner{},
		watcher:   NewWatcher(db, codec, dsn),
	}
}

//go:embed schema.sql
var schemaDDL string

// NewStoreFromDSN opens a *sql.DB, applies the schema, and returns a Store.
func NewStoreFromDSN(dsn string, codec runtime.Codec) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}
	if _, err := db.Exec(schemaDDL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return NewStore(db, codec, dsn), nil
}

// SetNewFunc sets the factory function for creating zero-value objects.
func (s *Store) SetNewFunc(f func() runtime.Object) {
	s.newFunc = f
}

// Stop signals the embedded watcher to terminate.
func (s *Store) Stop() {
	if s.watcher != nil {
		s.watcher.Stop()
	}
}

// Versioner returns the storage versioner.
func (s *Store) Versioner() storage.Versioner {
	return s.versioner
}

// Create inserts a new ResourceRelationship. It fails if the key already exists.
func (s *Store) Create(ctx context.Context, key string, obj, out runtime.Object, ttl uint64) error {
	if err := s.validateKey(key); err != nil {
		return err
	}
	rr, err := asResourceRelationship(obj)
	if err != nil {
		return err
	}

	// Ensure UID exists — apiserver normally fills this in, but a defensive
	// fallback keeps direct callers (tests, controllers) from inserting NULL.
	if rr.UID == "" {
		rr.UID = types.UID(uuid.NewUUID())
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to begin transaction: %w", err))
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			klog.ErrorS(err, "Failed to rollback transaction", "key", key)
		}
	}()

	rv := time.Now().UnixMilli()
	if err := s.versioner.UpdateObject(obj, uint64(rv)); err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to set resource version: %w", err))
	}

	data, err := runtime.Encode(s.codec, obj)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to encode object: %w", err))
	}

	if _, err := tx.ExecContext(ctx, insertRelationshipSQL, insertArgs(key, rv, rr, data)...); err != nil {
		if isUniqueViolation(err) {
			return storage.NewKeyExistsError(key, 0)
		}
		return storage.NewInternalError(fmt.Errorf("failed to insert relationship: %w", err))
	}

	if err := writeChangelog(ctx, tx, key, rv, "ADDED", data); err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to write changelog: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to commit transaction: %w", err))
	}

	return decode(s.codec, data, out, rv)
}

// Delete removes the relationship at the given key.
func (s *Store) Delete(ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions, validateDeletion storage.ValidateObjectFunc, cachedExistingObject runtime.Object, opts storage.DeleteOptions) error {
	if err := s.validateKey(key); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to begin transaction: %w", err))
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			klog.ErrorS(err, "Failed to rollback transaction", "key", key)
		}
	}()

	var existingData []byte
	var existingRV int64
	err = tx.QueryRowContext(ctx,
		`SELECT object_data, resource_version FROM resource_relationships WHERE key = $1 FOR UPDATE`,
		key,
	).Scan(&existingData, &existingRV)
	if err != nil {
		if err == sql.ErrNoRows {
			return storage.NewKeyNotFoundError(key, 0)
		}
		return storage.NewInternalError(fmt.Errorf("failed to read existing object: %w", err))
	}

	existing, err := decodeToObject(s.codec, existingData, s.newFunc)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to decode existing object: %w", err))
	}
	if err := s.versioner.UpdateObject(existing, uint64(existingRV)); err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to set resource version on existing: %w", err))
	}

	if preconditions != nil {
		if err := checkPreconditions(key, preconditions, existing); err != nil {
			return err
		}
	}
	if validateDeletion != nil {
		if err := validateDeletion(ctx, existing); err != nil {
			return err
		}
	}

	rv := time.Now().UnixMilli()

	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_relationships WHERE key = $1`, key); err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to delete relationship: %w", err))
	}

	if err := writeChangelog(ctx, tx, key, rv, "DELETED", existingData); err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to write changelog: %w", err))
	}

	if err := tx.Commit(); err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to commit transaction: %w", err))
	}

	return decode(s.codec, existingData, out, existingRV)
}

// Watch starts a watch stream on the given key.
func (s *Store) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	return s.watcher.Watch(ctx, key, opts, s.newFunc)
}

// Get retrieves an object by key.
func (s *Store) Get(ctx context.Context, key string, opts storage.GetOptions, objPtr runtime.Object) error {
	if err := s.validateKey(key); err != nil {
		return err
	}

	var data []byte
	var rv int64
	err := s.db.QueryRowContext(ctx,
		`SELECT object_data, resource_version FROM resource_relationships WHERE key = $1`,
		key,
	).Scan(&data, &rv)
	if err != nil {
		if err == sql.ErrNoRows {
			if opts.IgnoreNotFound {
				return runtime.SetZeroValue(objPtr)
			}
			return storage.NewKeyNotFoundError(key, 0)
		}
		return storage.NewInternalError(fmt.Errorf("failed to get object: %w", err))
	}

	return decode(s.codec, data, objPtr, rv)
}

// GetList retrieves a list of objects matching the key prefix and options.
func (s *Store) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to get items pointer: %w", err))
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return storage.NewInternalError(fmt.Errorf("need ptr to slice: %w", err))
	}

	var rows *sql.Rows
	if opts.Recursive {
		keyPrefix := key
		if !strings.HasSuffix(keyPrefix, "/") {
			keyPrefix += "/"
		}
		rows, err = s.db.QueryContext(ctx,
			`SELECT object_data, resource_version FROM resource_relationships WHERE key LIKE $1 ORDER BY key`,
			keyPrefix+"%",
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT object_data, resource_version FROM resource_relationships WHERE key = $1`,
			key,
		)
	}
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to list objects: %w", err))
	}
	defer rows.Close()

	for rows.Next() {
		var data []byte
		var rv int64
		if err := rows.Scan(&data, &rv); err != nil {
			return storage.NewInternalError(fmt.Errorf("failed to scan row: %w", err))
		}

		elem := reflect.New(v.Type().Elem())
		obj := elem.Interface().(runtime.Object)
		if err := decode(s.codec, data, obj, rv); err != nil {
			return storage.NewInternalError(fmt.Errorf("failed to decode list item: %w", err))
		}

		if !opts.Predicate.Empty() {
			matches, err := matchesPredicate(obj, opts.Predicate)
			if err != nil {
				return storage.NewInternalError(fmt.Errorf("predicate match: %w", err))
			}
			if !matches {
				continue
			}
		}
		v.Set(reflect.Append(v, elem.Elem()))
	}
	if err := rows.Err(); err != nil {
		return storage.NewInternalError(fmt.Errorf("error iterating rows: %w", err))
	}

	listRV, err := s.currentResourceVersion(ctx)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to get current resource version: %w", err))
	}
	if listAccessor, err := meta.ListAccessor(listObj); err == nil {
		listAccessor.SetResourceVersion(fmt.Sprintf("%d", listRV))
	}
	return nil
}

const guaranteedUpdateMaxBackoff = 640 * time.Millisecond

// GuaranteedUpdate performs a read-modify-write cycle with optimistic locking.
func (s *Store) GuaranteedUpdate(ctx context.Context, key string, destination runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, cachedExistingObject runtime.Object) error {
	if err := s.validateKey(key); err != nil {
		return err
	}

	const fallbackMaxAttempts = 10
	deadline, hasDeadline := ctx.Deadline()
	backoff := 10 * time.Millisecond

	for attempt := 0; ; attempt++ {
		data, rv, err := s.guaranteedUpdateOnce(ctx, key, destination, ignoreNotFound, preconditions, tryUpdate)
		if err == nil {
			return decode(s.codec, data, destination, rv)
		}
		if !storage.IsConflict(err) {
			return err
		}
		if hasDeadline {
			remaining := time.Until(deadline)
			if remaining < backoff+100*time.Millisecond {
				return err
			}
		} else if attempt >= fallbackMaxAttempts {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
		if next := backoff * 2; next < guaranteedUpdateMaxBackoff {
			backoff = next
		} else {
			backoff = guaranteedUpdateMaxBackoff
		}
	}
}

func (s *Store) guaranteedUpdateOnce(ctx context.Context, key string, destination runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc) ([]byte, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("failed to begin transaction: %w", err))
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			klog.ErrorS(err, "Failed to rollback transaction", "key", key)
		}
	}()

	var existingData []byte
	var existingRV int64
	err = tx.QueryRowContext(ctx,
		`SELECT object_data, resource_version FROM resource_relationships WHERE key = $1 FOR UPDATE`,
		key,
	).Scan(&existingData, &existingRV)

	var existing runtime.Object
	if err == sql.ErrNoRows {
		if !ignoreNotFound {
			return nil, 0, storage.NewKeyNotFoundError(key, 0)
		}
		existing = reflect.New(reflect.TypeOf(destination).Elem()).Interface().(runtime.Object)
	} else if err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("failed to read existing object: %w", err))
	} else {
		existing, err = decodeToObject(s.codec, existingData, s.newFunc)
		if err != nil {
			return nil, 0, storage.NewInternalError(fmt.Errorf("failed to decode existing object: %w", err))
		}
		if err := s.versioner.UpdateObject(existing, uint64(existingRV)); err != nil {
			return nil, 0, storage.NewInternalError(fmt.Errorf("failed to set resource version on existing: %w", err))
		}
	}

	if preconditions != nil {
		if err := checkPreconditions(key, preconditions, existing); err != nil {
			return nil, 0, err
		}
	}

	res := existing.DeepCopyObject()
	ret, _, err := tryUpdate(res, storage.ResponseMeta{ResourceVersion: uint64(existingRV)})
	if err != nil {
		return nil, 0, err
	}

	newData, err := runtime.Encode(s.codec, ret)
	if err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("failed to encode updated object: %w", err))
	}
	if existingData != nil && bytes.Equal(existingData, newData) {
		return existingData, existingRV, nil
	}

	rr, err := asResourceRelationship(ret)
	if err != nil {
		return nil, 0, err
	}
	if rr.UID == "" {
		rr.UID = types.UID(uuid.NewUUID())
	}

	rv := time.Now().UnixMilli()
	if rv <= existingRV {
		// Guarantee monotonicity even under same-millisecond churn.
		rv = existingRV + 1
	}
	if err := s.versioner.UpdateObject(ret, uint64(rv)); err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("failed to set resource version: %w", err))
	}

	data, err := runtime.Encode(s.codec, ret)
	if err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("failed to encode updated object: %w", err))
	}

	if existingData == nil {
		if _, err := tx.ExecContext(ctx, insertRelationshipSQL, insertArgs(key, rv, rr, data)...); err != nil {
			if isUniqueViolation(err) {
				return nil, 0, storage.NewKeyExistsError(key, 0)
			}
			return nil, 0, storage.NewInternalError(fmt.Errorf("failed to insert relationship: %w", err))
		}
		if err := writeChangelog(ctx, tx, key, rv, "ADDED", data); err != nil {
			return nil, 0, storage.NewInternalError(fmt.Errorf("failed to write changelog: %w", err))
		}
	} else {
		if _, err := tx.ExecContext(ctx, updateRelationshipSQL, updateArgs(key, rv, rr, data)...); err != nil {
			return nil, 0, storage.NewInternalError(fmt.Errorf("failed to update relationship: %w", err))
		}
		if err := writeChangelog(ctx, tx, key, rv, "MODIFIED", data); err != nil {
			return nil, 0, storage.NewInternalError(fmt.Errorf("failed to write changelog: %w", err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("failed to commit transaction: %w", err))
	}
	return data, rv, nil
}

// Count returns number of entries under the key (prefix).
func (s *Store) Count(key string) (int64, error) {
	ctx := context.Background()
	var count int64
	if key == "" {
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_relationships`).Scan(&count)
		if err != nil {
			return 0, storage.NewInternalError(fmt.Errorf("failed to count objects: %w", err))
		}
		return count, nil
	}
	keyPrefix := key
	if !strings.HasSuffix(keyPrefix, "/") {
		keyPrefix += "/"
	}
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_relationships WHERE key = $1 OR key LIKE $2`,
		key, keyPrefix+"%",
	).Scan(&count)
	if err != nil {
		return 0, storage.NewInternalError(fmt.Errorf("failed to count objects: %w", err))
	}
	return count, nil
}

// ReadinessCheck verifies the Postgres connection is healthy.
func (s *Store) ReadinessCheck() error {
	if v := s.readyErr.Load(); v != nil {
		return v.(*readyErrHolder).err
	}
	return s.db.Ping()
}

// RequestWatchProgress emits a bookmark advanced to the latest committed RV.
func (s *Store) RequestWatchProgress(ctx context.Context) error {
	rv, err := s.currentResourceVersion(ctx)
	if err != nil {
		return fmt.Errorf("postgres: failed to read current resource version: %w", err)
	}
	s.watcher.NotifyProgress(uint64(rv))
	return nil
}

// currentResourceVersion returns the highest RV durably committed in the
// changelog. Returns 1 if empty so the apiserver doesn't reject lists with
// "illegal resource version from storage: 0".
func (s *Store) currentResourceVersion(ctx context.Context) (int64, error) {
	var rv int64
	err := s.db.QueryRowContext(ctx,
		`SELECT GREATEST(COALESCE(MAX(resource_version), 0), 1)
		   FROM knowledge_changelog
		  WHERE commit_xid < pg_snapshot_xmin(pg_current_snapshot())::text::bigint`,
	).Scan(&rv)
	return rv, err
}

func (s *Store) validateKey(key string) error {
	if key == "" {
		return storage.NewInternalError(fmt.Errorf("key must not be empty"))
	}
	return nil
}

// asResourceRelationship type-asserts obj to *v1alpha1.ResourceRelationship.
// Returns a storage error if the object is not a ResourceRelationship — this
// Store is dedicated to that single kind.
func asResourceRelationship(obj runtime.Object) (*v1alpha1.ResourceRelationship, error) {
	rr, ok := obj.(*v1alpha1.ResourceRelationship)
	if !ok {
		return nil, storage.NewInternalError(fmt.Errorf("postgres store: expected *ResourceRelationship, got %T", obj))
	}
	return rr, nil
}

const insertRelationshipSQL = `
INSERT INTO resource_relationships (
    uid, key, name, namespace, resource_version, relationship_type,
    subject_api_group, subject_kind, subject_name, subject_namespace,
    subject_cp_context_kind, subject_cp_context_name,
    object_api_group, object_kind, object_name, object_namespace,
    object_cp_context_kind, object_cp_context_name,
    source_type, source_policy_ref, object_data, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12,
    $13, $14, $15, $16,
    $17, $18,
    $19, $20, $21, NOW(), NOW()
)`

const updateRelationshipSQL = `
UPDATE resource_relationships SET
    resource_version          = $1,
    relationship_type         = $2,
    subject_api_group         = $3,
    subject_kind              = $4,
    subject_name              = $5,
    subject_namespace         = $6,
    subject_cp_context_kind   = $7,
    subject_cp_context_name   = $8,
    object_api_group          = $9,
    object_kind               = $10,
    object_name               = $11,
    object_namespace          = $12,
    object_cp_context_kind    = $13,
    object_cp_context_name    = $14,
    source_type               = $15,
    source_policy_ref         = $16,
    object_data               = $17,
    updated_at                = NOW()
WHERE key = $18`

// insertArgs builds the positional arg list for insertRelationshipSQL.
// The positions match those of updateArgs so the two prepared statements
// can share a single helper.
func insertArgs(key string, rv int64, rr *v1alpha1.ResourceRelationship, data []byte) []any {
	return []any{
		string(rr.UID),          // $1  uid
		key,                     // $2  key
		rr.Name,                 // $3  name
		rr.Namespace,            // $4  namespace
		rv,                      // $5  resource_version
		rr.Spec.RelationshipType.Name, // $6
		rr.Spec.Subject.APIGroup,                       // $7
		rr.Spec.Subject.Kind,                           // $8
		rr.Spec.Subject.Name,                           // $9
		rr.Spec.Subject.Namespace,                      // $10
		rr.Spec.Subject.ControlPlaneContextRef.Kind,    // $11
		rr.Spec.Subject.ControlPlaneContextRef.Name,    // $12
		rr.Spec.Object.APIGroup,                        // $13
		rr.Spec.Object.Kind,                            // $14
		rr.Spec.Object.Name,                            // $15
		rr.Spec.Object.Namespace,                       // $16
		rr.Spec.Object.ControlPlaneContextRef.Kind,     // $17
		rr.Spec.Object.ControlPlaneContextRef.Name,     // $18
		string(rr.Spec.Source.Type),                    // $19
		policyRefJSON(rr.Spec.Source.PolicyRef),        // $20
		data,                                           // $21
	}
}

func updateArgs(key string, rv int64, rr *v1alpha1.ResourceRelationship, data []byte) []any {
	return []any{
		rv,                                              // $1  resource_version
		rr.Spec.RelationshipType.Name,                  // $2  relationship_type
		rr.Spec.Subject.APIGroup,                        // $3  subject_api_group
		rr.Spec.Subject.Kind,                            // $4  subject_kind
		rr.Spec.Subject.Name,                            // $5  subject_name
		rr.Spec.Subject.Namespace,                       // $6  subject_namespace
		rr.Spec.Subject.ControlPlaneContextRef.Kind,     // $7  subject_cp_context_kind
		rr.Spec.Subject.ControlPlaneContextRef.Name,     // $8  subject_cp_context_name
		rr.Spec.Object.APIGroup,                         // $9  object_api_group
		rr.Spec.Object.Kind,                             // $10 object_kind
		rr.Spec.Object.Name,                             // $11 object_name
		rr.Spec.Object.Namespace,                        // $12 object_namespace
		rr.Spec.Object.ControlPlaneContextRef.Kind,      // $13 object_cp_context_kind
		rr.Spec.Object.ControlPlaneContextRef.Name,      // $14 object_cp_context_name
		string(rr.Spec.Source.Type),                     // $15 source_type
		policyRefJSON(rr.Spec.Source.PolicyRef),         // $16 source_policy_ref
		data,                                            // $17 object_data
		key,                                             // $18 key (WHERE clause)
	}
}
