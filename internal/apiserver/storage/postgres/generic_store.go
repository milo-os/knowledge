package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/klog/v2"
)

// GenericStore implements storage.Interface against the knowledge_objects table.
// It is used for RelationshipType and RelationshipPolicy objects which do not
// need the BFS-optimised typed columns of resource_relationships.
type GenericStore struct {
	db        *sql.DB
	codec     runtime.Codec
	versioner storage.Versioner
	newFunc   func() runtime.Object
	watcher   *PostgresWatcher
}

// NewGenericStore creates a GenericStore using the knowledge_objects table.
func NewGenericStore(db *sql.DB, codec runtime.Codec, dsn string) *GenericStore {
	return &GenericStore{
		db:        db,
		codec:     codec,
		versioner: storage.APIObjectVersioner{},
		watcher:   NewWatcher(db, codec, dsn),
	}
}

// SetNewFunc sets the zero-value object factory.
func (s *GenericStore) SetNewFunc(f func() runtime.Object) { s.newFunc = f }

// Stop signals the embedded watcher to terminate.
func (s *GenericStore) Stop() {
	if s.watcher != nil {
		s.watcher.Stop()
	}
}

// Versioner returns the storage versioner.
func (s *GenericStore) Versioner() storage.Versioner { return s.versioner }

// Create inserts a new object into knowledge_objects.
func (s *GenericStore) Create(ctx context.Context, key string, obj, out runtime.Object, ttl uint64) error {
	rv := time.Now().UnixMilli()
	if err := s.versioner.UpdateObject(obj, uint64(rv)); err != nil {
		return storage.NewInternalError(fmt.Errorf("set resource version: %w", err))
	}
	data, err := runtime.Encode(s.codec, obj)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("encode object: %w", err))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("begin tx: %w", err))
	}
	defer rollback(tx, key)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO knowledge_objects (key, resource_version, object_data) VALUES ($1, $2, $3)`,
		key, rv, data,
	); err != nil {
		if isUniqueViolation(err) {
			return storage.NewKeyExistsError(key, 0)
		}
		return storage.NewInternalError(fmt.Errorf("insert object: %w", err))
	}
	if err := writeChangelog(ctx, tx, key, rv, "ADDED", data); err != nil {
		return storage.NewInternalError(fmt.Errorf("write changelog: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return storage.NewInternalError(fmt.Errorf("commit: %w", err))
	}
	return decode(s.codec, data, out, rv)
}

// Delete removes the object at key.
func (s *GenericStore) Delete(ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions, validateDeletion storage.ValidateObjectFunc, _ runtime.Object, _ storage.DeleteOptions) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("begin tx: %w", err))
	}
	defer rollback(tx, key)

	var existingData []byte
	var existingRV int64
	err = tx.QueryRowContext(ctx,
		`SELECT object_data, resource_version FROM knowledge_objects WHERE key = $1 FOR UPDATE`,
		key,
	).Scan(&existingData, &existingRV)
	if err == sql.ErrNoRows {
		return storage.NewKeyNotFoundError(key, 0)
	}
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("read existing: %w", err))
	}

	existing, err := decodeToObject(s.codec, existingData, s.newFunc)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("decode existing: %w", err))
	}
	_ = s.versioner.UpdateObject(existing, uint64(existingRV))

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
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_objects WHERE key = $1`, key); err != nil {
		return storage.NewInternalError(fmt.Errorf("delete object: %w", err))
	}
	if err := writeChangelog(ctx, tx, key, rv, "DELETED", existingData); err != nil {
		return storage.NewInternalError(fmt.Errorf("write changelog: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return storage.NewInternalError(fmt.Errorf("commit: %w", err))
	}
	return decode(s.codec, existingData, out, existingRV)
}

// Watch starts a watch on key.
func (s *GenericStore) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	return s.watcher.Watch(ctx, key, opts, s.newFunc)
}

// Get retrieves an object by key.
func (s *GenericStore) Get(ctx context.Context, key string, opts storage.GetOptions, objPtr runtime.Object) error {
	var data []byte
	var rv int64
	err := s.db.QueryRowContext(ctx,
		`SELECT object_data, resource_version FROM knowledge_objects WHERE key = $1`,
		key,
	).Scan(&data, &rv)
	if err == sql.ErrNoRows {
		if opts.IgnoreNotFound {
			return runtime.SetZeroValue(objPtr)
		}
		return storage.NewKeyNotFoundError(key, 0)
	}
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("get object: %w", err))
	}
	return decode(s.codec, data, objPtr, rv)
}

// GetList retrieves a list of objects matching the key prefix.
func (s *GenericStore) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) (err error) {
	listPtr, err := meta.GetItemsPtr(listObj)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("get items ptr: %w", err))
	}
	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return storage.NewInternalError(fmt.Errorf("need ptr to slice: %w", err))
	}

	var rows *sql.Rows
	if opts.Recursive {
		prefix := key
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		rows, err = s.db.QueryContext(ctx,
			`SELECT object_data, resource_version FROM knowledge_objects WHERE key LIKE $1 ORDER BY key`,
			prefix+"%",
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT object_data, resource_version FROM knowledge_objects WHERE key = $1`,
			key,
		)
	}
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("failed to list objects: %w", err))
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = storage.NewInternalError(fmt.Errorf("close rows: %w", cerr))
		}
	}()

	for rows.Next() {
		var data []byte
		var rv int64
		if err := rows.Scan(&data, &rv); err != nil {
			return storage.NewInternalError(fmt.Errorf("scan row: %w", err))
		}
		elem := reflect.New(v.Type().Elem())
		obj := elem.Interface().(runtime.Object)
		if err := decode(s.codec, data, obj, rv); err != nil {
			return storage.NewInternalError(fmt.Errorf("decode item: %w", err))
		}
		if !opts.Predicate.Empty() {
			matches, err := matchesPredicate(obj, opts.Predicate)
			if err != nil {
				return storage.NewInternalError(fmt.Errorf("predicate: %w", err))
			}
			if !matches {
				continue
			}
		}
		v.Set(reflect.Append(v, elem.Elem()))
	}
	if err := rows.Err(); err != nil {
		return storage.NewInternalError(fmt.Errorf("rows: %w", err))
	}

	listRV, err := s.currentResourceVersion(ctx)
	if err != nil {
		return storage.NewInternalError(fmt.Errorf("current rv: %w", err))
	}
	if la, err := meta.ListAccessor(listObj); err == nil {
		la.SetResourceVersion(fmt.Sprintf("%d", listRV))
	}
	return nil
}

// GuaranteedUpdate performs a read-modify-write with optimistic locking.
func (s *GenericStore) GuaranteedUpdate(ctx context.Context, key string, destination runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, _ runtime.Object) error {
	const maxAttempts = 10
	for range maxAttempts {
		data, rv, err := s.guaranteedUpdateOnce(ctx, key, destination, ignoreNotFound, preconditions, tryUpdate)
		if err == nil {
			return decode(s.codec, data, destination, rv)
		}
		if !storage.IsConflict(err) {
			return err
		}
	}
	return storage.NewInternalError(fmt.Errorf("too many conflicts updating %s", key))
}

func (s *GenericStore) guaranteedUpdateOnce(ctx context.Context, key string, destination runtime.Object, ignoreNotFound bool, preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc) ([]byte, int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("begin tx: %w", err))
	}
	defer rollback(tx, key)

	var existingData []byte
	var existingRV int64
	err = tx.QueryRowContext(ctx,
		`SELECT object_data, resource_version FROM knowledge_objects WHERE key = $1 FOR UPDATE`,
		key,
	).Scan(&existingData, &existingRV)

	var existing runtime.Object
	if err == sql.ErrNoRows {
		if !ignoreNotFound {
			return nil, 0, storage.NewKeyNotFoundError(key, 0)
		}
		existing = reflect.New(reflect.TypeOf(destination).Elem()).Interface().(runtime.Object)
	} else if err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("read existing: %w", err))
	} else {
		existing, err = decodeToObject(s.codec, existingData, s.newFunc)
		if err != nil {
			return nil, 0, storage.NewInternalError(fmt.Errorf("decode existing: %w", err))
		}
		_ = s.versioner.UpdateObject(existing, uint64(existingRV))
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
		return nil, 0, storage.NewInternalError(fmt.Errorf("encode updated: %w", err))
	}
	if existingData != nil && bytes.Equal(existingData, newData) {
		return existingData, existingRV, nil
	}

	rv := time.Now().UnixMilli()
	if rv <= existingRV {
		rv = existingRV + 1
	}
	if err := s.versioner.UpdateObject(ret, uint64(rv)); err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("set rv: %w", err))
	}
	data, err := runtime.Encode(s.codec, ret)
	if err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("encode: %w", err))
	}

	var eventType string
	if existingData == nil {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO knowledge_objects (key, resource_version, object_data) VALUES ($1, $2, $3)`,
			key, rv, data,
		)
		eventType = "ADDED"
	} else {
		_, err = tx.ExecContext(ctx,
			`UPDATE knowledge_objects SET resource_version = $2, object_data = $3 WHERE key = $1`,
			key, rv, data,
		)
		eventType = "MODIFIED"
	}
	if err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("upsert: %w", err))
	}

	if err := writeChangelog(ctx, tx, key, rv, eventType, data); err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("changelog: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, storage.NewInternalError(fmt.Errorf("commit: %w", err))
	}
	return data, rv, nil
}

// Count returns the number of objects under key.
func (s *GenericStore) Count(key string) (int64, error) {
	ctx := context.Background()
	var count int64
	prefix := key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM knowledge_objects WHERE key = $1 OR key LIKE $2`,
		key, prefix+"%",
	).Scan(&count)
	if err != nil {
		return 0, storage.NewInternalError(fmt.Errorf("count: %w", err))
	}
	return count, nil
}

// ReadinessCheck verifies the Postgres connection is healthy.
func (s *GenericStore) ReadinessCheck() error { return s.db.Ping() }

// RequestWatchProgress emits a bookmark for the latest RV.
func (s *GenericStore) RequestWatchProgress(ctx context.Context) error {
	rv, err := s.currentResourceVersion(ctx)
	if err != nil {
		return fmt.Errorf("postgres: current rv: %w", err)
	}
	s.watcher.NotifyProgress(uint64(rv))
	return nil
}

func (s *GenericStore) currentResourceVersion(ctx context.Context) (int64, error) {
	var rv int64
	err := s.db.QueryRowContext(ctx,
		`SELECT GREATEST(COALESCE(MAX(resource_version), 0), 1) FROM knowledge_changelog
		  WHERE commit_xid < pg_snapshot_xmin(pg_current_snapshot())::text::bigint`,
	).Scan(&rv)
	return rv, err
}

func rollback(tx *sql.Tx, key string) {
	if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
		klog.ErrorS(err, "Failed to rollback transaction", "key", key)
	}
}
