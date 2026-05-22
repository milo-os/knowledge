package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/klog/v2"
)

const (
	defaultPollInterval       = 5 * time.Second
	notifyCoalesceDelay       = 50 * time.Millisecond
	defaultBookmarkInterval   = 30 * time.Second
	defaultChangelogRetention = 5 * time.Minute
	defaultCleanupInterval    = 1 * time.Minute
	notifyChannelName         = "knowledge_changes"
	listenerMinReconnect      = 1 * time.Second
	listenerMaxReconnect      = 30 * time.Second
	horizonStallWarnInterval  = 5 * time.Minute
	pollBatchSize             = 500
)

// PostgresWatcher manages watch streams backed by the knowledge_changelog table.
type PostgresWatcher struct {
	db  *sql.DB
	codec runtime.Codec
	dsn   string

	cleanupOnce  sync.Once
	listenerOnce sync.Once
	cleanupDone  chan struct{}

	mu     sync.RWMutex
	active map[*knowledgeWatch]struct{}
}

// NewWatcher creates a new PostgresWatcher.
func NewWatcher(db *sql.DB, codec runtime.Codec, dsn string) *PostgresWatcher {
	return &PostgresWatcher{
		db:          db,
		codec:       codec,
		dsn:         dsn,
		cleanupDone: make(chan struct{}),
		active:      make(map[*knowledgeWatch]struct{}),
	}
}

// NotifyProgress pushes a fresh resource version to every active watch.
func (pw *PostgresWatcher) NotifyProgress(rv uint64) {
	pw.mu.RLock()
	defer pw.mu.RUnlock()
	for w := range pw.active {
		select {
		case w.progress <- rv:
		default:
		}
	}
}

// kickAll signals every active watch to immediately drain the changelog.
func (pw *PostgresWatcher) kickAll() {
	pw.mu.RLock()
	defer pw.mu.RUnlock()
	for w := range pw.active {
		select {
		case w.kick <- struct{}{}:
		default:
		}
	}
}

func (pw *PostgresWatcher) register(w *knowledgeWatch) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	pw.active[w] = struct{}{}
}

func (pw *PostgresWatcher) unregister(w *knowledgeWatch) {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	delete(pw.active, w)
}

// startListener launches a goroutine holding a dedicated pgx LISTEN connection.
func (pw *PostgresWatcher) startListener() {
	if pw.dsn == "" {
		klog.V(2).InfoS("PostgresWatcher: no DSN provided, LISTEN/NOTIFY disabled")
		return
	}

	go func() {
		klog.V(2).InfoS("PostgresWatcher: starting LISTEN/NOTIFY loop", "channel", notifyChannelName)
		defer klog.V(2).InfoS("PostgresWatcher: LISTEN/NOTIFY loop stopped")

		backoff := listenerMinReconnect
		for {
			select {
			case <-pw.cleanupDone:
				return
			default:
			}

			err := pw.runListener()
			if err == nil || errors.Is(err, context.Canceled) {
				return
			}

			klog.ErrorS(err, "PostgresWatcher: LISTEN connection lost, reconnecting", "backoff", backoff)
			select {
			case <-pw.cleanupDone:
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > listenerMaxReconnect {
				backoff = listenerMaxReconnect
			}
		}
	}()
}

// runListener opens a single pgx connection, issues LISTEN, and blocks.
func (pw *PostgresWatcher) runListener() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-pw.cleanupDone:
			cancel()
		case <-ctx.Done():
		}
	}()

	conn, err := pgx.Connect(ctx, pw.dsn)
	if err != nil {
		return fmt.Errorf("pgx connect: %w", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannelName); err != nil {
		return fmt.Errorf("LISTEN %s: %w", notifyChannelName, err)
	}
	klog.V(2).InfoS("PostgresWatcher: LISTEN connection established", "channel", notifyChannelName)

	pw.kickAll()

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("WaitForNotification: %w", err)
		}
		if n == nil {
			continue
		}
		pw.kickAll()
	}
}

// Watch starts a new watch stream for the given key prefix.
func (pw *PostgresWatcher) Watch(ctx context.Context, key string, opts storage.ListOptions, newFunc func() runtime.Object) (watch.Interface, error) {
	pw.cleanupOnce.Do(func() {
		go pw.cleanupLoop()
	})
	pw.listenerOnce.Do(func() {
		pw.startListener()
	})

	startRV := int64(0)
	if opts.ResourceVersion != "" {
		_, err := fmt.Sscanf(opts.ResourceVersion, "%d", &startRV)
		if err != nil {
			return nil, storage.NewInternalError(fmt.Errorf("invalid resource version %q: %w", opts.ResourceVersion, err))
		}
	}

	sendInitialEvents := opts.SendInitialEvents != nil && *opts.SendInitialEvents

	w := &knowledgeWatch{
		db:                pw.db,
		codec:             pw.codec,
		key:               key,
		predicate:         opts.Predicate,
		newFunc:           newFunc,
		startRV:           startRV,
		result:            make(chan watch.Event, 100),
		done:              make(chan struct{}),
		progress:          make(chan uint64, 1),
		kick:              make(chan struct{}, 1),
		sendInitialEvents: sendInitialEvents,
		parent:            pw,
	}

	if startRV > 0 {
		if err := w.seedCursorFromRV(ctx, startRV); err != nil {
			return nil, err
		}
	} else if !sendInitialEvents {
		// RV=0 without SendInitialEvents means "watch from now" — don't replay history.
		if err := w.seedCursorToNow(ctx); err != nil {
			return nil, err
		}
	}

	pw.register(w)
	go w.poll(ctx)
	return w, nil
}

// Stop terminates background goroutines.
func (pw *PostgresWatcher) Stop() {
	close(pw.cleanupDone)
}

// cleanupLoop periodically removes old changelog entries.
func (pw *PostgresWatcher) cleanupLoop() {
	ticker := time.NewTicker(defaultCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pw.cleanupDone:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-defaultChangelogRetention)
			result, err := pw.db.Exec(
				`DELETE FROM knowledge_changelog WHERE created_at < $1`, cutoff,
			)
			if err != nil {
				klog.ErrorS(err, "Failed to clean up changelog entries")
				continue
			}
			if rows, err := result.RowsAffected(); err == nil && rows > 0 {
				klog.V(2).InfoS("Cleaned up old changelog entries", "count", rows)
			}
		}
	}
}

// knowledgeWatch implements watch.Interface by polling the knowledge_changelog table.
type knowledgeWatch struct {
	db                *sql.DB
	codec             runtime.Codec
	key               string
	predicate         storage.SelectionPredicate
	newFunc           func() runtime.Object
	startRV           int64
	lastXid           int64
	lastID            int64
	lastRV            int64
	horizonLastAdvance     time.Time
	horizonAtLastAdvance   int64
	result            chan watch.Event
	done              chan struct{}
	progress          chan uint64
	kick              chan struct{}
	closeOnce         sync.Once
	sendInitialEvents bool
	parent            *PostgresWatcher
}

// ResultChan returns the channel of watch events.
func (w *knowledgeWatch) ResultChan() <-chan watch.Event {
	return w.result
}

// Stop stops the watch and releases resources.
func (w *knowledgeWatch) Stop() {
	w.closeOnce.Do(func() {
		if w.parent != nil {
			w.parent.unregister(w)
		}
		close(w.done)
	})
}

// poll continuously queries the changelog table for new events.
func (w *knowledgeWatch) poll(ctx context.Context) {
	defer close(w.result)

	w.horizonLastAdvance = time.Now()

	if w.sendInitialEvents {
		if err := w.sendInitialEventList(ctx); err != nil {
			klog.ErrorS(err, "Failed to send initial events", "key", w.key)
			return
		}
		if !w.sendInitialEventsEndBookmark() {
			return
		}
	}

	pollTicker := time.NewTicker(defaultPollInterval)
	defer pollTicker.Stop()

	bookmarkTicker := time.NewTicker(defaultBookmarkInterval)
	defer bookmarkTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.done:
			return
		case rv := <-w.progress:
			if err := w.drainChangelog(ctx); err != nil {
				klog.ErrorS(err, "Error draining changelog before progress bookmark", "key", w.key)
			}
			if rv > uint64(w.lastRV) {
				w.lastRV = int64(rv)
			}
			w.sendBookmarkAt(uint64(w.lastRV))
		case <-w.kick:
			coalesceTimer := time.NewTimer(notifyCoalesceDelay)
			select {
			case <-coalesceTimer.C:
			case <-ctx.Done():
				coalesceTimer.Stop()
				return
			case <-w.done:
				coalesceTimer.Stop()
				return
			}
			select {
			case <-w.kick:
			default:
			}
			if err := w.drainChangelog(ctx); err != nil {
				klog.ErrorS(err, "Error draining changelog after NOTIFY kick", "key", w.key)
			}
		case <-bookmarkTicker.C:
			w.sendBookmark()
		case <-pollTicker.C:
			if _, err := w.pollChanges(ctx); err != nil {
				klog.ErrorS(err, "Error polling changelog", "key", w.key)
			}
		}
	}
}

// seedCursorFromRV translates a client-supplied resource version into the
// internal (commit_xid, id) cursor.
func (w *knowledgeWatch) seedCursorFromRV(ctx context.Context, startRV int64) error {
	var xid, id sql.NullInt64
	err := w.db.QueryRowContext(ctx,
		`SELECT commit_xid, id FROM knowledge_changelog WHERE resource_version = $1 ORDER BY id DESC LIMIT 1`,
		startRV,
	).Scan(&xid, &id)
	if err != nil && err != sql.ErrNoRows {
		return storage.NewInternalError(fmt.Errorf("seed cursor from resource version %d: %w", startRV, err))
	}
	if err == sql.ErrNoRows {
		err = w.db.QueryRowContext(ctx,
			`SELECT commit_xid, id FROM knowledge_changelog
			  WHERE resource_version <= $1
			  ORDER BY resource_version DESC, id DESC
			  LIMIT 1`,
			startRV,
		).Scan(&xid, &id)
		if err != nil && err != sql.ErrNoRows {
			return storage.NewInternalError(fmt.Errorf("seed cursor from resource version <=%d: %w", startRV, err))
		}
	}
	if xid.Valid && id.Valid {
		w.lastXid = xid.Int64
		w.lastID = id.Int64
	} else {
		// The requested RV is not in the changelog (too old, already cleaned up).
		// Fall back to the current max position to avoid replaying all historical events.
		if err := w.seedCursorToNow(ctx); err != nil {
			return err
		}
	}
	w.lastRV = startRV
	return nil
}

// seedCursorToNow positions the watch cursor at the current end of the changelog.
// Used when the requested start RV cannot be found (e.g., cleaned up already).
func (w *knowledgeWatch) seedCursorToNow(ctx context.Context) error {
	var xid, id sql.NullInt64
	err := w.db.QueryRowContext(ctx,
		`SELECT commit_xid, id FROM knowledge_changelog ORDER BY commit_xid DESC, id DESC LIMIT 1`,
	).Scan(&xid, &id)
	if err != nil && err != sql.ErrNoRows {
		return storage.NewInternalError(fmt.Errorf("seed cursor to now: %w", err))
	}
	if xid.Valid && id.Valid {
		w.lastXid = xid.Int64
		w.lastID = id.Int64
	}
	return nil
}

// sendInitialEventList sends ADDED events for all existing objects matching the key prefix.
func (w *knowledgeWatch) sendInitialEventList(ctx context.Context) error {
	keyPrefix := w.key
	if !strings.HasSuffix(keyPrefix, "/") {
		keyPrefix += "/"
	}

	tx, err := w.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("failed to begin snapshot tx for initial list: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			klog.ErrorS(rbErr, "failed to rollback initial-list snapshot tx", "key", w.key)
		}
	}()

	var snapshotXmin int64
	if err := tx.QueryRowContext(ctx,
		`SELECT pg_snapshot_xmin(pg_current_snapshot())::text::bigint`,
	).Scan(&snapshotXmin); err != nil {
		return fmt.Errorf("failed to capture snapshot xmin: %w", err)
	}

	var cursorXid, cursorID sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT commit_xid, id
		   FROM knowledge_changelog
		  WHERE commit_xid < $1
		  ORDER BY commit_xid DESC, id DESC
		  LIMIT 1`,
		snapshotXmin,
	).Scan(&cursorXid, &cursorID); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to capture initial cursor: %w", err)
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT key, resource_version, object_data
		   FROM resource_relationships
		  WHERE key = $1 OR key LIKE $2
		  ORDER BY resource_version ASC`,
		w.key, keyPrefix+"%",
	)
	if err != nil {
		return fmt.Errorf("failed to query objects for initial events: %w", err)
	}

	for rows.Next() {
		var key string
		var rv int64
		var data []byte

		if err := rows.Scan(&key, &rv, &data); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan object row: %w", err)
		}

		event, err := w.toWatchEvent("ADDED", data, rv)
		if err != nil {
			klog.ErrorS(err, "Failed to convert object to initial event", "key", key, "rv", rv)
			continue
		}

		if !w.predicate.Empty() {
			matches, err := w.predicate.Matches(event.Object)
			if err != nil || !matches {
				if rv > w.lastRV {
					w.lastRV = rv
				}
				continue
			}
		}

		select {
		case w.result <- *event:
			if rv > w.lastRV {
				w.lastRV = rv
			}
		case <-w.done:
			rows.Close()
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit initial list snapshot: %w", err)
	}

	w.lastXid = cursorXid.Int64
	w.lastID = cursorID.Int64
	return nil
}

// sendInitialEventsEndBookmark sends a bookmark event with the k8s.io/initial-events-end annotation.
func (w *knowledgeWatch) sendInitialEventsEndBookmark() bool {
	if w.newFunc == nil {
		klog.ErrorS(nil, "Cannot send initial-events-end bookmark: newFunc is nil", "key", w.key)
		return false
	}

	var maxRV int64
	if err := w.db.QueryRow(
		`SELECT COALESCE(MAX(resource_version), 0)
		   FROM knowledge_changelog
		  WHERE commit_xid < pg_snapshot_xmin(pg_current_snapshot())::text::bigint`,
	).Scan(&maxRV); err != nil {
		klog.ErrorS(err, "Failed to get committed max resource version for initial-events-end bookmark")
		return false
	}
	if maxRV > w.lastRV {
		w.lastRV = maxRV
	}

	obj := w.newFunc()
	versioner := storage.APIObjectVersioner{}
	if err := versioner.UpdateObject(obj, uint64(w.lastRV)); err != nil {
		klog.ErrorS(err, "Failed to set resource version on bookmark object")
		return false
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		klog.ErrorS(err, "Failed to get meta accessor for bookmark object")
		return false
	}
	annotations := accessor.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["k8s.io/initial-events-end"] = "true"
	accessor.SetAnnotations(annotations)

	select {
	case w.result <- watch.Event{Type: watch.Bookmark, Object: obj}:
		return true
	case <-w.done:
		return false
	}
}

// drainChangelog calls pollChanges until fewer than pollBatchSize rows are returned.
func (w *knowledgeWatch) drainChangelog(ctx context.Context) error {
	for {
		n, err := w.pollChanges(ctx)
		if err != nil {
			return err
		}
		if n < pollBatchSize {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.done:
			return nil
		default:
		}
	}
}

// pollChanges fetches new changelog entries since the last emitted (commit_xid, id) cursor.
func (w *knowledgeWatch) pollChanges(ctx context.Context) (int, error) {
	keyPrefix := w.key
	if !strings.HasSuffix(keyPrefix, "/") {
		keyPrefix += "/"
	}

	var horizon int64
	if err := w.db.QueryRowContext(ctx,
		`SELECT pg_snapshot_xmin(pg_current_snapshot())::text::bigint`,
	).Scan(&horizon); err != nil {
		return 0, fmt.Errorf("failed to read snapshot horizon: %w", err)
	}

	w.maybeWarnHorizonStall(horizon)

	rows, err := w.db.QueryContext(ctx,
		`SELECT key, resource_version, event_type, data, commit_xid, id, created_at
		   FROM knowledge_changelog
		  WHERE commit_xid < $1
		    AND (commit_xid > $2 OR (commit_xid = $2 AND id > $3))
		    AND (key = $4 OR key LIKE $5)
		  ORDER BY commit_xid ASC, id ASC
		  LIMIT $6`,
		horizon, w.lastXid, w.lastID, w.key, keyPrefix+"%", pollBatchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to query changelog: %w", err)
	}
	defer rows.Close()

	var n int
	for rows.Next() {
		var key string
		var rv int64
		var eventType string
		var data []byte
		var xid, id int64
		var createdAt time.Time

		if err := rows.Scan(&key, &rv, &eventType, &data, &xid, &id, &createdAt); err != nil {
			return n, fmt.Errorf("failed to scan changelog row: %w", err)
		}

		event, err := w.toWatchEvent(eventType, data, rv)
		if err != nil {
			klog.ErrorS(err, "Failed to convert changelog entry to watch event",
				"key", key, "rv", rv, "eventType", eventType)
			w.advanceCursor(xid, id, rv)
			n++
			continue
		}

		if !w.predicate.Empty() {
			matches, err := w.predicate.Matches(event.Object)
			if err != nil || !matches {
				w.advanceCursor(xid, id, rv)
				n++
				continue
			}
		}

		select {
		case w.result <- *event:
			w.advanceCursor(xid, id, rv)
			n++
		case <-w.done:
			return n, nil
		}
	}
	return n, rows.Err()
}

// advanceCursor moves the emitted-so-far cursor to (xid, id).
func (w *knowledgeWatch) advanceCursor(xid, id, rv int64) {
	w.lastXid = xid
	w.lastID = id
	if rv > w.lastRV {
		w.lastRV = rv
	}
}

// maybeWarnHorizonStall logs a WARN if the snapshot horizon has not moved forward.
func (w *knowledgeWatch) maybeWarnHorizonStall(horizon int64) {
	now := time.Now()
	if horizon > w.horizonAtLastAdvance {
		w.horizonAtLastAdvance = horizon
		w.horizonLastAdvance = now
		return
	}
	if now.Sub(w.horizonLastAdvance) >= horizonStallWarnInterval {
		klog.Warningf("PostgresWatcher: snapshot horizon frozen at xid8 %d for %s; a long-running transaction is blocking newer events for key=%q",
			horizon, now.Sub(w.horizonLastAdvance).Round(time.Second), w.key)
		w.horizonLastAdvance = now
	}
}

// toWatchEvent converts a changelog row into a watch.Event.
func (w *knowledgeWatch) toWatchEvent(eventType string, data []byte, rv int64) (*watch.Event, error) {
	var watchType watch.EventType
	switch eventType {
	case "ADDED":
		watchType = watch.Added
	case "MODIFIED":
		watchType = watch.Modified
	case "DELETED":
		watchType = watch.Deleted
	default:
		return nil, fmt.Errorf("unknown event type: %s", eventType)
	}

	if data == nil {
		return nil, fmt.Errorf("changelog entry has nil data")
	}

	out := w.newFunc()
	obj, _, err := w.codec.Decode(data, nil, out)
	if err != nil {
		return nil, fmt.Errorf("failed to decode changelog data: %w", err)
	}

	versioner := storage.APIObjectVersioner{}
	if err := versioner.UpdateObject(obj, uint64(rv)); err != nil {
		return nil, fmt.Errorf("failed to set resource version: %w", err)
	}

	return &watch.Event{
		Type:   watchType,
		Object: obj,
	}, nil
}

// sendBookmark sends a periodic bookmark event reflecting the latest committed RV.
func (w *knowledgeWatch) sendBookmark() {
	var maxRV int64
	err := w.db.QueryRow(
		`SELECT COALESCE(MAX(resource_version), 0)
		   FROM knowledge_changelog
		  WHERE commit_xid < pg_snapshot_xmin(pg_current_snapshot())::text::bigint`,
	).Scan(&maxRV)
	if err != nil {
		klog.ErrorS(err, "Failed to get max resource version for bookmark")
		return
	}
	if maxRV <= w.lastRV {
		return
	}
	w.sendBookmarkAt(uint64(maxRV))
}

// sendBookmarkAt emits a bookmark event with the supplied resource version.
func (w *knowledgeWatch) sendBookmarkAt(rv uint64) {
	if w.newFunc == nil {
		return
	}
	obj := w.newFunc()
	versioner := storage.APIObjectVersioner{}
	if err := versioner.UpdateObject(obj, rv); err != nil {
		klog.ErrorS(err, "Failed to set resource version on bookmark object")
		return
	}
	event := watch.Event{Type: watch.Bookmark, Object: obj}
	select {
	case w.result <- event:
		if int64(rv) > w.lastRV {
			w.lastRV = int64(rv)
		}
	case <-w.done:
	default:
	}
}
