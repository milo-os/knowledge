-- Knowledge service storage schema.
--
-- The primary data table is `resource_relationships` — one row per
-- ResourceRelationship CRD instance. Typed columns are extracted from the
-- object spec so BFS traversal queries can use indexes instead of scanning
-- JSON. The full object is also persisted in `object_data` (JSONB) so
-- the storage.Interface contract (Get/List returns full objects) is met.
--
-- Watch support is provided via `knowledge_changelog`: every mutation
-- inserts a row, an AFTER INSERT trigger fires pg_notify('knowledge_changes'),
-- and the apiserver's PostgresWatcher tails this table with an xmin-horizon
-- cursor to guarantee commit-ordered delivery.

CREATE TABLE IF NOT EXISTS resource_relationships (
    uid                         TEXT PRIMARY KEY,
    key                         TEXT NOT NULL UNIQUE,
    name                        TEXT NOT NULL,
    namespace                   TEXT NOT NULL,
    resource_version            BIGINT NOT NULL,
    relationship_type           TEXT NOT NULL,
    subject_api_group           TEXT NOT NULL,
    subject_kind                TEXT NOT NULL,
    subject_name                TEXT NOT NULL,
    subject_namespace           TEXT NOT NULL DEFAULT '',
    subject_cp_context_kind     TEXT NOT NULL,
    subject_cp_context_name     TEXT NOT NULL,
    object_api_group            TEXT NOT NULL,
    object_kind                 TEXT NOT NULL,
    object_name                 TEXT NOT NULL,
    object_namespace            TEXT NOT NULL DEFAULT '',
    object_cp_context_kind      TEXT NOT NULL,
    object_cp_context_name      TEXT NOT NULL,
    source_type                 TEXT NOT NULL,
    source_policy_ref           JSONB,
    object_data                 JSONB NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_rr_namespace_name
    ON resource_relationships (namespace, name);

CREATE INDEX IF NOT EXISTS idx_rr_subject
    ON resource_relationships (subject_api_group, subject_kind, subject_namespace, subject_name, subject_cp_context_kind, subject_cp_context_name);

CREATE INDEX IF NOT EXISTS idx_rr_object
    ON resource_relationships (object_api_group, object_kind, object_namespace, object_name, object_cp_context_kind, object_cp_context_name);

CREATE INDEX IF NOT EXISTS idx_rr_type
    ON resource_relationships (relationship_type);

CREATE INDEX IF NOT EXISTS idx_rr_namespace
    ON resource_relationships (namespace);

-- Generic object store for non-BFS types (RelationshipType, RelationshipPolicy).
-- These use simple JSONB persistence; the relationship_type/resource_relationships
-- table is reserved for the BFS-optimised typed columns.
CREATE TABLE IF NOT EXISTS knowledge_objects (
    key              TEXT PRIMARY KEY,
    resource_version BIGINT NOT NULL,
    object_data      JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_knowledge_objects_prefix
    ON knowledge_objects (key text_pattern_ops);

-- Changelog table for Watch support (LISTEN/NOTIFY pattern from ipam).
CREATE TABLE IF NOT EXISTS knowledge_changelog (
    id               BIGSERIAL PRIMARY KEY,
    key              TEXT NOT NULL,
    resource_version BIGINT NOT NULL,
    event_type       TEXT NOT NULL CHECK (event_type IN ('ADDED', 'MODIFIED', 'DELETED')),
    data             BYTEA,
    commit_xid       BIGINT NOT NULL DEFAULT (pg_current_xact_id()::text::bigint),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_knowledge_changelog_key
    ON knowledge_changelog (key);

CREATE INDEX IF NOT EXISTS idx_knowledge_changelog_rv
    ON knowledge_changelog (resource_version);

CREATE INDEX IF NOT EXISTS idx_knowledge_changelog_xid_id
    ON knowledge_changelog (commit_xid, id);

-- Trigger function for LISTEN/NOTIFY: every changelog insert wakes watchers.
CREATE OR REPLACE FUNCTION knowledge_notify_change() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('knowledge_changes', NEW.resource_version::text);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS knowledge_changelog_notify ON knowledge_changelog;
CREATE TRIGGER knowledge_changelog_notify
    AFTER INSERT ON knowledge_changelog
    FOR EACH ROW EXECUTE FUNCTION knowledge_notify_change();
