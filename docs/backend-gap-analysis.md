# Backend Gap Analysis — Knowledge Graph UI

Audit of each UI screen's data needs against the existing `knowledge.miloapis.com/v1alpha1` API surface.

## API surface summary

| Resource | Scope | Verbs | LIST filtering |
|---|---|---|---|
| `RelationshipType` | Cluster | get, list, watch, create, update, delete | label selector; field selector limited to `metadata.name` |
| `RelationshipPolicy` | Namespaced | get, list, watch, create, update, delete | label selector; field selector limited to `metadata.name`, `metadata.namespace` |
| `ResourceRelationship` | Namespaced | get, list, watch, create, update, delete | label selector; field selector limited to `metadata.name`, `metadata.namespace` |
| `GraphQuery` | Namespaced | **create only** (SAR pattern, never persisted) | n/a |

Key observations from source:
- `internal/apiserver/storage/strategy.go:57` — `getAttrs` exposes ONLY `metadata.name` and `metadata.namespace` as field selectors. No selectable fields on spec (e.g., no `spec.subject.kind`, `spec.relationshipType.name`, `spec.source.type`).
- `internal/apiserver/storage/postgres/store.go:295` — LIST applies the selection predicate **after** loading all rows from Postgres. Filtering happens in-process; there is no pushdown to indexed columns despite those columns existing in the schema.
- `internal/apiserver/storage/postgres/store.go:220` — Watch is supported via `PostgresWatcher` (LISTEN/NOTIFY + polling fallback). Standard k8s watch semantics apply.
- `internal/apiserver/graphquery/rest.go:60` — `GraphQuery.Create` is synchronous; result is in the response body. The object is never persisted, so there is no history/listing of past queries.
- `pkg/apis/knowledge/v1alpha1/types_graphquery.go:17` — `MaxNodes` is capped at **1000** (not 500 as the UI banner copy implies); default 100.
- `bfs/bfs.go:78` — Truncation flag is set ONLY when `maxNodes` is exceeded; reaching `maxDepth` does not flag truncation, which may surprise the UI.
- The knowledge graph stores **edges only**, not the content of referenced resources. There is no API to fetch resource labels/annotations/conditions of an arbitrary endpoint Kind through this service.

---

## Screen 1 — Graph Explorer

### Supported
- Firing a GraphQuery on a chosen root: `POST /apis/knowledge.miloapis.com/v1alpha1/namespaces/{ns}/graphqueries` with the spec built from user input. Response is the populated `Status` (nodes, edges, truncated).
- Filter by relationship type: `spec.traverse.relationshipTypes` (server-side BFS filter).
- Color-code by `ControlPlaneContextRef`: each node carries `endpoint.controlPlaneContextRef.{kind,name}` in the response — pure client-side concern.
- Direction (`Outbound`/`Inbound`/`Both`): supported by the BFS engine.
- Truncation banner: server returns `status.truncated` when `maxNodes` is exceeded.

### Partially supported
- **Truncated flag semantics** — only fires on `maxNodes`, never on `maxDepth`. The user could hit max depth and silently see a clipped graph. *Severity: medium. Recommendation: extend BFS to also set `truncated=true` (or add a distinct `depthLimitReached` field) when the depth loop exits with a non-empty frontier. Workaround: UI can detect this client-side if the deepest depth equals `maxDepth`.*
- **Node limit banner copy ("500 node limit")** — the actual cap is 1000, default 100. *Severity: low. Recommendation: align UI copy to use the actual `maxNodes` value the user submitted, or change the design to read the value from the spec.*

### Missing
- **Context bands (visual layering of org/project nodes)** — purely a rendering decision; the API provides the data needed. *No gap.*
- **Persisting / replaying a previous query** — `GraphQuery` is create-only and never stored. *Severity: low. Recommendation: defer; treat as client-side history (localStorage) of recent query specs.*

---

## Screen 2 — Node Detail Drawer

### Supported
- **Resource identity** (Kind, Name, Namespace, APIGroup, ControlPlaneContextRef) — already present on every `GraphNode.endpoint` in the graph response. No follow-up API call needed.
- **Connections tab (inbound + outbound edges)** — achievable by issuing a focused `GraphQuery` with `root=<clicked endpoint>`, `maxDepth=1`, `direction=Both`. This returns exactly the inbound + outbound edges incident to that node. Alternative: the parent graph response already contains all edges and the UI can filter locally to ones referencing this node.

### Partially supported
- (none)

### Missing — BLOCKING
- **Metadata tab (labels, annotations, creationTimestamp, conditions of the referenced resource)** — the knowledge graph stores edges only; it does not know anything about the contents of the arbitrary `Kind` an endpoint refers to (e.g., a `Domain`, a `Gateway`, a `User`). This service has no proxy to other API servers/control planes either.
  - *Severity: **blocking** for the Metadata tab as designed.*
  - *Recommendations, in preference order:*
    1. **Remove the Metadata tab from MVP** and show only Connections. Best fit for current scope.
    2. **Client-side fetch**: have the UI call the appropriate per-Kind API directly (e.g., the Organization or Project control plane API) using the `controlPlaneContextRef` to route. This requires the UI to authenticate against multiple control planes and know each Kind's endpoint — significant frontend complexity.
    3. **Add a backend proxy endpoint** in the knowledge service that takes a `ResourceEndpoint` and proxies a GET to the appropriate control plane. Largest backend effort; turns the knowledge service into a cross-context resource fetcher, which it is not today.

---

## Screen 3 — Graph Query Builder modal

### Supported
- All form fields map 1:1 to `GraphQuerySpec`:
  - Root: `spec.root.{apiGroup,kind,name,namespace,controlPlaneContextRef.{kind,name}}` — all required by `validateRoot` in `rest.go:133`.
  - MaxDepth: `spec.traverse.maxDepth` (max 20, default 5).
  - MaxNodes: `spec.traverse.maxNodes` (max 1000, default 100).
  - Direction: `spec.traverse.direction` (`Outbound|Inbound|Both`, default Outbound).
  - RelationshipTypes filter: `spec.traverse.relationshipTypes`.
- Synchronous result in HTTP response.

### Partially supported
- **Discovering valid Kinds / APIGroups / ControlPlaneContextRefs to populate dropdowns** — there is no enumeration endpoint. The UI can derive a partial Kind list from `LIST relationshiptypes` (each lists `subjectGVK`/`objectGVK`), but that only covers Kinds that participate in some declared relationship. Organization/Project names must come from the parent Milo platform API, not this service. *Severity: medium. Recommendation: populate Kind dropdowns from `RelationshipType` enumeration; let user free-type Name; populate ControlPlaneContext lists from the Milo platform API (separate concern from this service).*

### Missing
- (none beyond the above)

---

## Screen 4 — Relationship Inventory

### Supported
- LIST `ResourceRelationship` across a namespace: `GET /apis/.../namespaces/{ns}/resourcerelationships` (or all namespaces). Pagination via standard k8s `limit`/`continue`.
- Age is derivable from `metadata.creationTimestamp`.
- Total count: standard k8s `LIST` does not return a total, but the UI can use `?limit=1` and inspect `metadata.remainingItemCount` (k8s sends this on paginated lists). *Note: `remainingItemCount` is best-effort and only populated when there's a continue token.*
- Delete: standard `DELETE` verb.

### Partially supported — HIGH
- **Filter by RelationshipType name / Subject kind / Source type** — these are spec fields, and field selectors are restricted to `metadata.name`/`metadata.namespace` (see `strategy.go:57`). Options:
  1. **Client-side filter** after `LIST` — works for small datasets but does not scale; the design shows "247 relationships" which suggests the dataset could be large. Pagination interacts badly with client-side filters (filter only sees the current page).
  2. **Add field selectors** for `spec.relationshipType.name`, `spec.subject.kind`, `spec.source.type`. The Postgres schema already indexes these columns (`relationship_type`, `subject_kind`, `source_type`), so pushdown would be a real win. Requires extending `getAttrs` and the storage layer to honour field selectors with a SQL `WHERE` clause.
  3. **Encode filters as labels**: have the controller/policy reconciler stamp labels like `knowledge.miloapis.com/relationship-type=foo` on each `ResourceRelationship`. Label selectors already work end-to-end. Lower-effort but adds bookkeeping.
  - *Severity: high. Recommendation: **option 3 (label stamping) for MVP**, then option 2 (proper field selector pushdown) as a follow-up. Option 1 (client-side only) is unacceptable for the "247 relationships" scenario.*

### Partially supported — MEDIUM
- **Sort by Age / RelationshipType** — the k8s API does not support server-side sorting. *Severity: medium. Recommendation: sort client-side within the current page; document that sort is page-local; or sort the entire dataset by fetching with sufficient page size.*

### Missing
- **Exact total count display ("247 relationships")** — see above; `remainingItemCount` is best-effort and only on paginated lists. *Severity: low. Recommendation: rely on `remainingItemCount + len(items)` and accept some inaccuracy; or add a dedicated `/count` subresource if exact counts matter.*

---

## Screen 5 — Types & Policies Catalog

### Supported
- LIST `RelationshipType` (cluster-scoped).
- LIST `RelationshipPolicy` (namespaced) — name (`metadata.name`), RelationshipType (`spec.relationshipType.name`), Subject kind (`spec.subject.kind`), DiscoveredEdgesCount (`status.discoveredEdgesCount`) all present.
- Create `RelationshipPolicy` via standard `POST` — CEL `expression` field is straightforward.
- Delete `RelationshipPolicy` via standard `DELETE`.

### Partially supported
- (none)

### Missing
- **Creating a `RelationshipType` from the UI is NOT in the design (read-only)** — the API does support CREATE on RelationshipType (cluster-scoped). This is a deliberate UX choice, not a backend gap. *Severity: low. Recommendation: keep RelationshipType creation out of the UI; declare types via cluster config / GitOps. Note this in the UX spec so users aren't surprised.*
- **CEL expression validation feedback** — there is no dry-run endpoint to validate a CEL expression before creating a policy. The controller reconciles asynchronously and reports errors via `status.conditions`. *Severity: medium. Recommendation: defer dry-run; UI should poll the created policy's status conditions after submission and surface any compile/eval errors.*

---

## Cross-cutting concerns

### Watch / real-time updates
- **Supported.** The Postgres-backed storage implements `Watch` via `PostgresWatcher` (LISTEN/NOTIFY with polling fallback). The UI can use the standard k8s `?watch=true&resourceVersion=N` pattern to keep the Relationship Inventory and Catalog screens live-updating.
- *Recommendation: use watch on Screens 4 and 5. The graph (Screen 1) is a point-in-time snapshot — no live update is necessary, and re-running the query is the natural refresh.*

### GraphQuery history
- **Not stored.** SAR pattern is intentional. *Severity: low. Recommendation: client-side recent-queries list in localStorage; defer any server-side history.*

### `direction=Both` and `maxNodes`
- Both align with the design: `Both` is fully implemented in BFS (UNION of subject- and object-side joins). `maxNodes` cap of 1000 is well above any reasonable UI rendering ceiling. **The UI design's "500 node limit" banner copy is a hardcoded UX assumption that doesn't match the server cap** — recommend the UI read the user's submitted `maxNodes` value and substitute it into the banner.

### Auth
- Standard k8s bearer-token auth applies. The UI must supply a token with appropriate RBAC for each resource (`relationshiptypes`, `relationshippolicies`, `resourcerelationships`, `graphqueries`). No special auth handling required by the knowledge service beyond what k8s already provides.

---

## Consolidated gap list (sorted by severity)

| # | Screen | Gap | Severity | Recommendation |
|---|---|---|---|---|
| 1 | 2 | Metadata tab needs resource content the knowledge service doesn't store | **Blocking** | Remove Metadata tab from MVP (preferred) |
| 2 | 4 | No server-side filtering on `relationshipType` / `subject.kind` / `source.type` | **High** | Label-stamp via controller for MVP; later add field-selector pushdown |
| 3 | 1 | Truncated flag doesn't fire on maxDepth | Medium | Add depth-limit flag, or detect client-side |
| 4 | 3 | No enumeration of valid Kinds/ControlPlaneContextRefs | Medium | Derive Kinds from RelationshipType list; pull ControlPlaneContexts from Milo platform API |
| 5 | 4 | No server-side sort | Medium | Sort client-side within the current page; document |
| 6 | 5 | No CEL expression dry-run | Medium | Poll `status.conditions` after CREATE; defer dry-run endpoint |
| 7 | 1 | Banner says "500 node limit" but server cap is 1000 (user-configurable) | Low | UI should read the user's submitted `maxNodes` value |
| 8 | 4 | No exact total count | Low | Use `remainingItemCount + len(items)`; accept best-effort |
| 9 | 1 | No GraphQuery history | Low | Client-side recent-queries list (localStorage) |
| 10 | 5 | RelationshipType create not in UI | Low | Intentional — keep out of UI; declare via cluster config |
