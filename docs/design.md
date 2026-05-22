# Knowledge Graph Service — Design Document

**API Group:** `knowledge.miloapis.com/v1alpha1`
**Status:** Draft
**Date:** 2026-05-19

---

## Table of Contents

1. [Overview & Motivation](#1-overview--motivation)
2. [Goals & Non-Goals](#2-goals--non-goals)
3. [Architecture Overview](#3-architecture-overview)
4. [API Types](#4-api-types)
5. [Auto-Discovery](#5-auto-discovery)
6. [Storage Layer](#6-storage-layer)
7. [Graph Query Execution](#7-graph-query-execution)
8. [Cross-Context Relationships](#8-cross-context-relationships)
9. [Authorization](#9-authorization)
10. [Multi-Tenancy](#10-multi-tenancy)
11. [Service Architecture](#11-service-architecture)
12. [End-to-End Test Environment](#12-end-to-end-test-environment)
13. [Future: Backend Migration](#13-future-backend-migration)

---

## 1. Overview & Motivation

Milo OS is a Kubernetes-native business operating system built on `k8s.io/apiserver`, controller-runtime, and a multicluster-runtime provider. Each Project gets its own virtual Kubernetes control plane. Resources are organized in a four-level hierarchy:

```
Platform → Organization → Project → Resources
```

Kubernetes `ownerReferences` are the built-in mechanism for expressing relationships between objects, but they are insufficient for the use cases Milo OS needs to support (see [datum-cloud/datum#164](https://github.com/datum-cloud/datum/discussions/164)):

- **Ownership only:** `ownerReferences` model parent-owns-child relationships and drive garbage collection. They cannot represent arbitrary typed edges such as "Domain is attached to Gateway" or "Service depends on Database."
- **No edge metadata:** There is no standard way to annotate the relationship with a type, direction, cardinality constraint, or provenance.
- **No graph traversal:** Kubernetes has no first-class API for querying transitive relationships. Finding all resources reachable from a root requires client-side fan-out across many object types.
- **Single-namespace:** `ownerReferences` only work within a single namespace. Cross-project and cross-organization relationships cannot be expressed.
- **No auto-discovery:** There is no mechanism to derive relationships from field values (e.g., a `gatewayRef` field) without writing bespoke controllers that each independently manage their own relationship stores.

The Knowledge Graph Service introduces a purpose-built relationship layer that is additive to (and does not replace) the existing ownership graph. It provides:

- **Typed, directed edges** between any two resources across any control plane context.
- **Policy-driven auto-discovery** of relationships from field values using CEL expressions.
- **Synchronous graph traversal** scoped to what the requesting principal can access.
- **A single consistent storage model** backed by PostgreSQL with a clear migration path to a dedicated graph database.

---

## 2. Goals & Non-Goals

### Goals

- Provide `RelationshipType` as a cluster-scoped schema for declaring the semantics, cardinality, and direction of an edge type.
- Provide `RelationshipPolicy` as a namespaced CRD that auto-discovers edges by evaluating a CEL expression against watched resources.
- Provide `ResourceRelationship` as the authoritative, persisted record of a single directed edge between two resources.
- Provide `GraphQuery` as a synchronous, create-only aggregated API resource for performing bounded BFS traversal.
- Store edges in PostgreSQL using the same `pgx/v5` + `storage.Interface` pattern already used by `ipam` and `automation`.
- Enforce tenant isolation via the existing control plane context routing — no additional row-level security.
- Support cross-context edges (cross-project, cross-org, platform-level) with a clearly defined Lowest Common Ancestor (LCA) storage rule.
- Remain authorization-transparent: the existing Milo auth chain scopes traversal to what the caller can access with no additional OpenFGA calls per edge.

### Non-Goals

- Replacing Kubernetes `ownerReferences` or interfering with the garbage collector's ownership graph.
- Supporting asynchronous or streaming query results in v1alpha1 (GraphQuery is synchronous and request-scoped).
- Providing a dedicated graph query language (e.g., Cypher, Gremlin). CEL handles discovery; the GraphQuery API handles traversal.
- Implementing row-level security in Postgres. Tenant isolation is handled at the storage partition level by control plane context routing.
- Providing general-purpose graph analytics (shortest path, centrality, etc.) in v1alpha1.

---

## 3. Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                          Milo OS Control Plane                       │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │               Aggregated API Server                          │    │
│  │  (knowledge.miloapis.com/v1alpha1)                           │    │
│  │                                                              │    │
│  │   CRD resources (via kube-apiserver):                        │    │
│  │     RelationshipType  (cluster-scoped)                       │    │
│  │     RelationshipPolicy (namespaced)                          │    │
│  │                                                              │    │
│  │   Aggregated API resources (via knowledge apiserver):        │    │
│  │     ResourceRelationship  ──────────────────────┐           │    │
│  │     GraphQuery (create-only, SAR pattern)        │           │    │
│  └─────────────────────────────────────────────────┼───────────┘    │
│                                                     │                │
│  ┌──────────────────────┐              ┌────────────▼─────────────┐  │
│  │   Controller Manager │              │   PostgreSQL             │  │
│  │                      │              │                          │  │
│  │  RelationshipPolicy  │  writes      │  resource_relationships  │  │
│  │  reconciler ─────────┼─────────────►  table (per cluster_id)  │  │
│  │                      │              │                          │  │
│  │  ResourceRelationship│  reads       │  Indexed on:             │  │
│  │  validity controller │◄─────────────┤  (cluster_id, source_id) │  │
│  │                      │              │  (cluster_id, target_id) │  │
│  │  Leader election     │              │  (relationship_type)     │  │
│  └──────────────────────┘              └──────────────────────────┘  │
│                                                     ▲                │
│                              BFS traversal          │                │
│                      (batched SQL per depth level)  │                │
│                              GraphQuery handler ────┘                │
└──────────────────────────────────────────────────────────────────────┘

Control plane context routing:
  Platform namespace  → stores cross-org and platform-level edges
  Org namespace       → stores cross-project (same-org) edges
  Project namespace   → stores same-project edges
```

The knowledge service follows the same structural pattern as `ipam` and `automation`: a standalone aggregated API server process and a separate controller-manager process, sharing a PostgreSQL backend. The aggregated API server exposes `ResourceRelationship` and `GraphQuery`; the Kubernetes API server serves the CRD types (`RelationshipType`, `RelationshipPolicy`).

---

## 4. API Types

All types are in the API group `knowledge.miloapis.com/v1alpha1`.

### 4.1 RelationshipType (cluster-scoped CRD)

`RelationshipType` is a cluster-scoped resource created by service providers (platform operators). It declares the schema for an edge type: which resource kinds may appear on each endpoint, the cardinality, and whether the edge is directed or undirected.

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: RelationshipType
metadata:
  name: attached-to
spec:
  name: AttachedTo
  subject:
    apiGroups: ["networking.miloapis.com"]
    kinds: ["Domain", "HTTPRoute"]
  object:
    apiGroups: ["networking.miloapis.com"]
    kinds: ["Gateway", "Connector"]
  cardinality: ManyToOne
  direction: Directed
```

**Field descriptions:**

| Field | Type | Description |
|---|---|---|
| `spec.name` | string | Pascal-case canonical name referenced by `RelationshipPolicy` and `GraphQuery`. Must be unique across the cluster. |
| `spec.subject.apiGroups` | []string | API groups permitted for the subject (source) endpoint of this relationship type. |
| `spec.subject.kinds` | []string | Resource kinds permitted for the subject endpoint. |
| `spec.object.apiGroups` | []string | API groups permitted for the object (target) endpoint. |
| `spec.object.kinds` | []string | Resource kinds permitted for the object endpoint. |
| `spec.cardinality` | enum | One of `OneToOne`, `OneToMany`, `ManyToOne`, `ManyToMany`. Informational; not enforced at storage time in v1alpha1. |
| `spec.direction` | enum | `Directed` (edges have a subject→object orientation) or `Undirected` (edges are traversed in both directions regardless of storage orientation). |

### 4.2 RelationshipPolicy (namespaced CRD)

`RelationshipPolicy` is a namespaced resource that instructs the controller to watch a specific resource kind and evaluate a CEL expression for each instance. The expression returns a list of object references; the controller creates or deletes `ResourceRelationship` objects to match.

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: RelationshipPolicy
metadata:
  name: domain-gateway-attachment
  namespace: organization-datum
spec:
  subject:
    apiVersion: networking.miloapis.com/v1alpha1
    kind: Domain
  relationshipType: AttachedTo
  discovery:
    expression: |
      subject.spec.?gatewayRef.orValue(null) != null
        ? [subject.spec.gatewayRef]
        : []
```

**Field descriptions:**

| Field | Type | Description |
|---|---|---|
| `spec.subject.apiVersion` | string | The API version of the resource kind to watch. |
| `spec.subject.kind` | string | The resource kind to watch. Every event on this kind triggers CEL evaluation. |
| `spec.relationshipType` | string | The `spec.name` of a `RelationshipType` that the discovered edges must conform to. |
| `spec.discovery.expression` | string | A CEL expression evaluated with `subject` bound to the watched resource (as a map). Must return a list of object references (each a map with `apiGroup`, `kind`, `name`, `namespace`, and optionally `controlPlaneContext`). An empty list means no edges for this instance. |

The `discovery.expression` field is the single, uniform mechanism for all discovery patterns. CEL's optional chaining (`?field`), `orValue`, and list comprehensions handle all cases uniformly — there are no shorthand alternatives.

CEL evaluation follows the same cost-limiting pattern used in `milo/internal/quota/engine/cel.go`. Expressions exceeding the configured cost limit are rejected with a validation error at admission time.

### 4.3 ResourceRelationship (namespaced, aggregated API)

`ResourceRelationship` is the authoritative record of a single typed edge between two resources. It is stored in PostgreSQL and served via the knowledge aggregated API server. Objects are written by the RelationshipPolicy controller (source: `Policy`) or by human/automated operators (source: `Manual`).

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: ResourceRelationship
metadata:
  name: domain-datum-net--attached-to--gateway-production
  namespace: organization-datum
spec:
  relationshipType: AttachedTo
  subject:
    apiGroup: networking.miloapis.com
    kind: Domain
    name: datum.net
    namespace: project-xyz
    controlPlaneContext:
      kind: Project
      name: my-production-project
  object:
    apiGroup: networking.miloapis.com
    kind: Gateway
    name: production-website
    namespace: project-xyz
    controlPlaneContext:
      kind: Project
      name: my-production-project
  source:
    type: Policy
    policyRef:
      name: domain-gateway-attachment
      namespace: organization-datum
status:
  conditions:
    - type: Valid
      status: "True"
```

**Field descriptions:**

| Field | Type | Description |
|---|---|---|
| `spec.relationshipType` | string | The `spec.name` of the `RelationshipType` this edge instantiates. |
| `spec.subject` | ResourceEndpoint | The source endpoint of the edge. |
| `spec.subject.apiGroup` | string | API group of the subject resource. |
| `spec.subject.kind` | string | Kind of the subject resource. |
| `spec.subject.name` | string | Name of the subject resource. |
| `spec.subject.namespace` | string | Namespace of the subject resource within its control plane context. |
| `spec.subject.controlPlaneContext` | ControlPlaneContextRef | Which control plane (Project, Organization, Platform) owns the subject. |
| `spec.object` | ResourceEndpoint | The target endpoint of the edge. Same fields as `spec.subject`. |
| `spec.source.type` | enum | `Policy` (managed by a RelationshipPolicy) or `Manual` (created directly). |
| `spec.source.policyRef` | ObjectRef | Present when `type: Policy`. Points to the owning `RelationshipPolicy`. |
| `status.conditions` | []Condition | Standard Kubernetes condition list. `Valid` condition indicates the edge references a known `RelationshipType` and both endpoint kinds are permitted by that type. |

The `ControlPlaneContextRef` type:

```go
type ControlPlaneContextRef struct {
    // +kubebuilder:validation:Enum=Platform;Organization;Project;User
    Kind string `json:"kind,omitempty"`
    Name string `json:"name,omitempty"`  // empty for Platform
}
```

This follows the existing `ParentContext` enum vocabulary from `pkg/server/discovery/contexts.go` and `GrantParentContextSpec` from the quota subsystem.

### 4.4 GraphQuery (create-only, aggregated API)

`GraphQuery` follows the SubjectAccessReview pattern: a caller POSTs a spec and receives the result synchronously in the HTTP response. The object is never persisted. This avoids the complexity of async polling while keeping the API surface Kubernetes-idiomatic.

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: GraphQuery
metadata:
  name: domain-topology
spec:
  root:
    apiGroup: networking.miloapis.com
    kind: Domain
    name: datum.net
    controlPlaneContext:
      kind: Project
      name: my-production-project
  traverse:
    relationshipTypes: ["AttachedTo", "RouteTo"]
    direction: Outbound
    maxDepth: 5
    maxNodes: 500
# status populated synchronously in the response:
status:
  nodes:
    - apiGroup: networking.miloapis.com
      kind: Gateway
      name: production-website
      controlPlaneContext:
        kind: Project
        name: my-production-project
  edges:
    - subject: {kind: Domain, name: datum.net}
      object: {kind: Gateway, name: production-website}
      relationshipType: AttachedTo
```

**Field descriptions:**

| Field | Type | Description |
|---|---|---|
| `spec.root` | ResourceEndpoint | The starting node for the traversal. |
| `spec.traverse.relationshipTypes` | []string | Filter traversal to only these relationship type names. Empty means traverse all types. |
| `spec.traverse.direction` | enum | `Outbound` (follow subject→object edges), `Inbound` (follow object→subject edges), or `Both`. |
| `spec.traverse.maxDepth` | int | Maximum number of hops from the root. Hard upper bound enforced by the server. |
| `spec.traverse.maxNodes` | int | Maximum number of nodes to return. Traversal stops when this limit is reached. |
| `status.nodes` | []ResourceEndpoint | All nodes discovered during traversal, excluding the root. |
| `status.edges` | []EdgeRecord | All edges traversed, with subject, object, and relationship type. |

`maxDepth` and `maxNodes` together bound worst-case latency: at most `maxDepth` round trips to Postgres and at most `maxNodes` rows in memory at any time.

---

## 5. Auto-Discovery

### How RelationshipPolicy Works

The RelationshipPolicy controller is a standard controller-runtime reconciler with a dynamic watch. On startup (or when a `RelationshipPolicy` is created/updated) it establishes a watch on the `spec.subject` kind within the same namespace scope as the policy.

For each resource event (create, update, delete):

1. The controller evaluates `spec.discovery.expression` with `subject` bound to the full resource object (unstructured map).
2. The expression returns a `[]map` of object references, each containing at minimum `apiGroup`, `kind`, and `name`.
3. The controller computes the desired set of `ResourceRelationship` objects and reconciles against the actual set:
   - **New targets:** create a `ResourceRelationship` with `source.type: Policy`.
   - **Removed targets:** delete the corresponding `ResourceRelationship`.
   - **Unchanged targets:** no-op.
4. On resource deletion, all `ResourceRelationship` objects created by this policy for this subject are deleted.

### CEL Expression Semantics

The `subject` variable in the CEL expression is the full Kubernetes resource as a `map(string, dyn)`, matching the CEL representation used throughout the Milo quota engine.

Expressions must return `list(map(string, dyn))` where each map represents one object endpoint. Optional chaining (`?field`), `orValue`, `filter`, and `map` comprehensions cover all practical discovery patterns without needing shorthand variants.

Examples:

```cel
# Single optional ref field
subject.spec.?gatewayRef.orValue(null) != null
  ? [subject.spec.gatewayRef]
  : []

# List of refs
subject.spec.backends.map(b, b.serviceRef)

# Conditional with filter
subject.spec.rules
  .filter(r, r.?targetRef.orValue(null) != null)
  .map(r, r.targetRef)
```

CEL expression cost is evaluated at admission time using the `cel.NewEnv` + `cel.EstimateCost` API, following the pattern in `milo/internal/quota/engine/cel.go`. Expressions exceeding the cost limit fail admission with a descriptive validation error.

### Leader Election

The RelationshipPolicy controller writes `ResourceRelationship` objects. Multiple replicas of the controller-manager participate in leader election (standard `controller-runtime` lease-based election). Only the leader runs the policy reconciler and writes edges. All replicas can serve read queries against Postgres directly.

---

## 6. Storage Layer

### PostgreSQL Schema

`ResourceRelationship` objects are persisted in PostgreSQL using the same `pgx/v5` + `storage.Interface` pattern established by `ipam/internal/storage/postgres/` and `automation/internal/apiserver/storage/postgres/store.go`.

The primary table follows the tenant-scoped storage pattern: a `cluster_id` column (the control plane context identifier, equivalent to the `cluster_id` column in `automation`) partitions rows by tenant. No cross-tenant joins are ever executed.

Key columns and indexes:

```sql
CREATE TABLE resource_relationships (
    uid              UUID PRIMARY KEY,
    cluster_id       TEXT NOT NULL,
    name             TEXT NOT NULL,
    namespace        TEXT NOT NULL,
    resource_version BIGINT NOT NULL,
    relationship_type TEXT NOT NULL,
    subject_api_group TEXT NOT NULL,
    subject_kind     TEXT NOT NULL,
    subject_name     TEXT NOT NULL,
    subject_namespace TEXT,
    subject_context_kind TEXT,
    subject_context_name TEXT,
    object_api_group  TEXT NOT NULL,
    object_kind       TEXT NOT NULL,
    object_name       TEXT NOT NULL,
    object_namespace  TEXT,
    object_context_kind TEXT,
    object_context_name TEXT,
    source_type       TEXT NOT NULL,
    source_policy_name TEXT,
    source_policy_namespace TEXT,
    object_data      JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_rr_cluster_subject
    ON resource_relationships (cluster_id, subject_kind, subject_name, subject_namespace);

CREATE INDEX idx_rr_cluster_object
    ON resource_relationships (cluster_id, object_kind, object_name, object_namespace);

CREATE INDEX idx_rr_cluster_type
    ON resource_relationships (cluster_id, relationship_type);
```

The `object_data JSONB` column stores the full serialized `ResourceRelationship` object for `storage.Interface` compatibility (list, watch, get operations return the full object).

### BFS Traversal Algorithm

Graph traversal is implemented at the application layer in `internal/bfs/`. The algorithm is a standard breadth-first search with batched SQL queries per depth level:

```
func Traverse(ctx, root, filter, maxDepth, maxNodes) (nodes, edges):
    visited = {root.uid}
    frontier = [root]
    result_nodes = []
    result_edges = []

    for depth in 0..maxDepth:
        if len(frontier) == 0:
            break

        -- One SQL query fetches all edges where source_id = ANY(frontier_ids)
        -- filtered by cluster_id (partition) and optionally relationship_type
        rows = db.Query("""
            SELECT * FROM resource_relationships
            WHERE cluster_id = $1
              AND subject_uid = ANY($2)
              AND ($3::text[] IS NULL OR relationship_type = ANY($3))
        """, clusterID, frontierUIDs, typeFilter)

        next_frontier = []
        for row in rows:
            if row.object_uid not in visited:
                visited.add(row.object_uid)
                result_nodes.append(row.object)
                result_edges.append(row)
                next_frontier.append(row.object)
                if len(result_nodes) >= maxNodes:
                    return result_nodes, result_edges

        frontier = next_frontier

    return result_nodes, result_edges
```

This is `O(maxDepth)` SQL round trips with one batched query per level. It avoids recursive CTEs, which are harder to bound and less portable to future backends.

For `Inbound` traversal, the query filters on `object_uid = ANY($1)` instead of `subject_uid`. For `Both`, both queries are issued and results are merged per depth level.

### Backend Swappability

The `storage.Interface` abstraction decouples the BFS engine and the REST handlers from the underlying database. The interface is identical to the one used by `ipam` and `automation`:

```go
type Interface interface {
    Create(ctx, obj) error
    Update(ctx, obj) error
    Delete(ctx, key) error
    Get(ctx, key) (obj, error)
    List(ctx, opts) (list, error)
    Watch(ctx, opts) (watch.Interface, error)
}
```

Migrating to a dedicated graph store requires only:
1. A new implementation of `storage.Interface` targeting the new backend.
2. A one-time reindex: replay all existing `RelationshipPolicy` controllers against the existing control plane resources to regenerate edges in the new store.

No changes to the REST handlers, BFS engine, or controller logic are needed.

---

## 7. Graph Query Execution

### SAR Pattern

`GraphQuery` follows the SubjectAccessReview (SAR) pattern documented in `milo/vendor/k8s.io/kubernetes/pkg/registry/authorization/subjectaccessreview/rest.go` and implemented for Milo in `milo/internal/apiserver/identity/serviceaccountkeys/rest.go`.

The REST handler is registered as a create-only resource. When a POST arrives:

1. The handler deserializes the `GraphQuery` spec.
2. It validates `maxDepth` and `maxNodes` against configured server-side limits (preventing DoS via unbounded traversal).
3. It resolves the root node's `controlPlaneContext` to a `cluster_id`.
4. It calls the BFS engine with the resolved `cluster_id`, type filter, direction, and bounds.
5. It populates `status.nodes` and `status.edges` on the in-memory object.
6. It returns the fully populated object in the HTTP response with status `201 Created`.

The object is never written to storage. From the caller's perspective the API is Kubernetes-idiomatic: POST a spec-only object, receive a status-populated object back.

### Latency Bounds

With `maxDepth = D` and `maxNodes = N`:
- At most `D` SQL queries are issued (one per BFS level).
- At most `N` rows are materialized in memory.
- Each SQL query is a single indexed lookup: `WHERE cluster_id = $1 AND subject_uid = ANY($2)`.

For typical values (`maxDepth=5`, `maxNodes=500`) against a well-indexed Postgres instance, p99 latency is expected to be well under 500ms. Server-side defaults and hard caps can be configured per deployment.

### Error Handling

- `maxDepth` or `maxNodes` exceeding server-side hard limits → `400 Bad Request` with a descriptive message.
- Root node's `controlPlaneContext` not resolvable → `422 Unprocessable Entity`.
- Traversal hitting `maxNodes` before exhausting the graph → result is returned with a `status.truncated: true` field indicating the result is incomplete.

---

## 8. Cross-Context Relationships

### ControlPlaneContextRef

Every endpoint in a `ResourceRelationship` carries a `ControlPlaneContextRef`:

```go
type ControlPlaneContextRef struct {
    // +kubebuilder:validation:Enum=Platform;Organization;Project;User
    Kind string `json:"kind,omitempty"`
    Name string `json:"name,omitempty"`  // empty for Platform
}
```

This uses the same `Kind` enum values as `ParentContext` in `pkg/server/discovery/contexts.go` and `GrantParentContextSpec` in the quota subsystem, ensuring vocabulary consistency across Milo OS.

### Lowest Common Ancestor (LCA) Storage Rule

A `ResourceRelationship` is always stored in the control plane that is the Lowest Common Ancestor of its two endpoints. This rule determines the `cluster_id` (Kubernetes namespace) used for storage and the namespace field of the `ResourceRelationship` object itself.

| Subject context | Object context | Storage location |
|---|---|---|
| Project A (Org X) | Project A (Org X) | Project A control plane |
| Project A (Org X) | Project B (Org X) | Organization X control plane |
| Project A (Org X) | Project C (Org Y) | Platform control plane |
| Organization X | Organization Y | Platform control plane |
| Platform | Any | Platform control plane |
| Any | Platform | Platform control plane |

**Rationale:** Storing cross-project (same-org) edges at the Organization level, rather than Platform level, prevents watch stream fanout to all tenants. A platform-level watch would deliver all relationship events to all organizations; storing at the org level confines watch traffic to the affected organization's control plane partition.

### Cross-Context Client Resolution

The RelationshipPolicy controller resolves cross-context clients using the same pattern as `milo/internal/quota/controllers/policy/parent_context_resolver.go`. Given two `ControlPlaneContextRef` values, the controller:

1. Determines the LCA context.
2. Resolves a `client.Client` for that context via the multicluster-runtime provider.
3. Writes or deletes the `ResourceRelationship` in that context's namespace.

GraphQuery traversal similarly resolves `cluster_id` values per BFS level, issuing queries against the appropriate Postgres partition for each hop.

---

## 9. Authorization

### No Per-Edge OpenFGA Calls

The knowledge service does not issue OpenFGA authorization checks during graph traversal. Doing so would add one network round-trip per edge discovered, making traversal latency proportional to graph size — unacceptable for deep or wide graphs.

### Auth via Control Plane Context Routing

Authorization is enforced structurally by the control plane context routing layer, the same mechanism that protects all other Milo OS resources:

- The caller authenticates to the Milo API server via the standard bearer token / client certificate path.
- The context routing decorators determine which control plane partitions the caller can access (their Project, Organization, or Platform context).
- `ResourceRelationship` objects are stored in and queried from the partition corresponding to their LCA control plane context.
- A caller with access only to their own project's control plane cannot issue queries against the organization or platform partition.

This means the set of edges returned by a `GraphQuery` is exactly the set of edges stored in partitions the caller can reach — the same boundary enforcement as any other Kubernetes resource type. No additional filtering is needed.

### RBAC

Standard Kubernetes RBAC governs create/get/list/watch/delete on `ResourceRelationship` and `GraphQuery`:

- Service providers (platform operators) get full access via ClusterRole.
- Tenant users get read access to `ResourceRelationship` in their project/org namespace, and create access to `GraphQuery` in their namespace.
- The RelationshipPolicy controller's service account gets create/update/delete on `ResourceRelationship` across contexts it manages.

---

## 10. Multi-Tenancy

### Partition-per-Context Model

Milo OS uses the projectMux pattern: each Project, Organization, and the Platform each have their own virtual Kubernetes control plane (their own etcd/apiserver namespace in the underlying infrastructure). The knowledge service follows this model exactly.

Each control plane context maps to a `cluster_id` in PostgreSQL (following the pattern in `automation/internal/apiserver/storage/postgres/store.go`). All SQL queries include a `WHERE cluster_id = $1` predicate, confining each query to a single tenant's data.

Tenant isolation is therefore enforced at two levels:
1. **API layer:** The context routing decorators reject requests from callers without access to the requested partition.
2. **Storage layer:** All queries are scoped by `cluster_id`; the application never issues cross-`cluster_id` joins.

### No Row-Level Security Required

Because every query is already scoped by `cluster_id` at the application layer, PostgreSQL row-level security (RLS) is not needed. This matches the approach used by `ipam` and `automation`. If a future compliance requirement mandates defense-in-depth at the database layer, RLS policies can be added without changing the application code.

### Edge Placement and Watch Traffic

Placing cross-project (same-org) edges in the organization partition rather than the platform partition is critical for scalability. A platform-level watch would route all relationship events to all tenants sharing the platform control plane. By using the LCA rule, watch traffic for cross-project edges is confined to the organization whose projects are involved, matching the locality of other organization-scoped resources.

---

## 11. Service Architecture

### Components

```
knowledge/
├── cmd/knowledge/
│   ├── apiserver/          # Aggregated API server
│   └── controller-manager/ # RelationshipPolicy + validity controllers
│
├── pkg/apis/knowledge/v1alpha1/
│   ├── relationshiptype_types.go
│   ├── relationshippolicy_types.go
│   ├── resourcerelationship_types.go
│   ├── graphquery_types.go
│   └── register.go
│
├── internal/
│   ├── apiserver/
│   │   ├── storage/postgres/   # storage.Interface backed by pgx/v5
│   │   └── graphquery/         # create-only REST handler (SAR pattern)
│   │
│   ├── controllers/
│   │   ├── policy/             # RelationshipPolicy reconciler
│   │   └── relationship/       # ResourceRelationship validity controller
│   │
│   └── bfs/                    # BFS traversal engine (batched SQL per hop)
│
├── config/
│   ├── crd/                    # Generated CRD manifests
│   ├── apiserver/              # Deployment, Service, RBAC
│   ├── controller-manager/     # Deployment, RBAC
│   └── samples/                # Example resources
│
└── test/
    ├── e2e/                    # Chainsaw test suites
    └── kind/                   # Kind cluster config + setup scripts
```

### Aggregated API Server (`cmd/knowledge/apiserver`)

- Registers `ResourceRelationship` with a PostgreSQL-backed `storage.Interface` (wired via the `GenericStorageProviders` pattern from `milo/cmd/milo/apiserver/config.go`).
- Registers `GraphQuery` as a create-only REST handler (no storage backend) using the SAR pattern from `milo/internal/apiserver/identity/serviceaccountkeys/rest.go`.
- Runs as a horizontally scalable Deployment (no leader election needed; all instances are read-capable and handle GraphQuery requests independently).

### Controller Manager (`cmd/knowledge/controller-manager`)

- Runs the `RelationshipPolicy` reconciler: watches source resource kinds, evaluates CEL, writes/deletes `ResourceRelationship` objects via the aggregated API server.
- Runs the `ResourceRelationship` validity controller: watches `ResourceRelationship` objects and updates `status.conditions[Valid]` based on whether the referenced `RelationshipType` exists and the endpoint kinds are permitted.
- Leader election via standard controller-runtime lease: only the leader runs the policy reconciler. Multiple replicas improve resilience; the non-leader replicas are idle on the write path but remain available to take over immediately.

### Storage Provider Wiring

The PostgreSQL storage provider is wired following `milo/internal/apiserver/storage/identity/storageprovider.go`. A `StorageProvider` struct holds a `pgx.Pool` and implements a `ResourceRelationshipStore` that satisfies `storage.Interface`. The BFS traversal engine receives the pool directly (not through `storage.Interface`) to issue batched multi-row queries efficiently.

---

## 12. End-to-End Test Environment

### Kind Cluster Setup

E2E tests run against a local Kind cluster configured to host the Milo OS control plane. The setup mirrors the pattern established by other Milo services:

```
test/
└── kind/
    ├── kind-config.yaml        # Multi-node Kind cluster configuration
    └── setup.sh                # Installs CRDs, deploys knowledge service, seeds test data
```

The `setup.sh` script:
1. Creates a Kind cluster with the provided config.
2. Applies the generated CRD manifests from `config/crd/`.
3. Deploys the aggregated API server and controller-manager from `config/apiserver/` and `config/controller-manager/`.
4. Applies sample `RelationshipType` resources that use only native Kubernetes types (see below).

### Native Kubernetes Relationship Types for Testing

To avoid depending on `networking.miloapis.com` or other Milo service CRDs in tests, the E2E suite defines `RelationshipType` and `RelationshipPolicy` resources that operate on native Kubernetes types:

```yaml
# ConfigMap references a Secret
apiVersion: knowledge.miloapis.com/v1alpha1
kind: RelationshipType
metadata:
  name: references-secret
spec:
  name: ReferencesSecret
  subject:
    apiGroups: [""]
    kinds: ["ConfigMap"]
  object:
    apiGroups: [""]
    kinds: ["Secret"]
  cardinality: ManyToMany
  direction: Directed
---
# Pod is scheduled on a Node
apiVersion: knowledge.miloapis.com/v1alpha1
kind: RelationshipType
metadata:
  name: scheduled-on
spec:
  name: ScheduledOn
  subject:
    apiGroups: [""]
    kinds: ["Pod"]
  object:
    apiGroups: [""]
    kinds: ["Node"]
  cardinality: ManyToOne
  direction: Directed
```

Corresponding `RelationshipPolicy` resources use CEL expressions against standard Kubernetes fields (`spec.nodeName`, `spec.volumes[*].secret.secretName`, etc.).

### Chainsaw Test Suites

Tests are written using [Chainsaw](https://kyverno.github.io/chainsaw/) (the same framework used for other Milo service E2E tests) and live in `test/e2e/`. Each test case follows the Chainsaw apply-assert pattern:

```
test/e2e/
├── relationship-type/
│   ├── chainsaw-test.yaml      # Create RelationshipType, assert validation
│   └── samples/
├── policy-discovery/
│   ├── chainsaw-test.yaml      # Create policy + source resource, assert edge created
│   └── samples/
├── graph-query/
│   ├── chainsaw-test.yaml      # POST GraphQuery, assert status.nodes/edges
│   └── samples/
└── cross-context/
    ├── chainsaw-test.yaml      # Cross-project edges, assert LCA placement
    └── samples/
```

Key test scenarios:

1. **RelationshipType validation:** Verify that endpoint kind mismatches are rejected at admission.
2. **Policy discovery (create):** Create a source resource with a populated ref field; assert that a `ResourceRelationship` is created within the reconcile timeout.
3. **Policy discovery (update):** Update the ref field to a different target; assert the old edge is deleted and a new one is created.
4. **Policy discovery (delete):** Delete the source resource; assert all edges created by the policy for that resource are deleted.
5. **GraphQuery bounded traversal:** Build a known graph (N nodes, D depth); assert that `GraphQuery` with various `maxDepth`/`maxNodes` values returns the expected subgraph.
6. **GraphQuery truncation:** Build a graph wider than `maxNodes`; assert `status.truncated: true`.
7. **Cross-context LCA placement:** Create a relationship between resources in two different namespaces; assert the `ResourceRelationship` lands in the correct LCA namespace.

---

## 13. Future: Backend Migration

### Why ArangoDB

PostgreSQL is a mature, operationally familiar backend and sufficient for the initial workload. However, multi-hop BFS traversal in a relational store requires one SQL round-trip per depth level, and deep or wide graphs will eventually saturate connection pools. ArangoDB is the preferred candidate for a future migration because:

- It is a native multi-model graph database with first-class AQL graph traversal (`FOR v, e IN 1..N OUTBOUND startVertex GRAPH 'myGraph'`).
- Traversal is executed entirely in the database engine, eliminating application-level round-trips per hop.
- It supports the same document model as the current Postgres `object_data JSONB` column, simplifying migration.
- Its Kubernetes operator is production-ready and fits the Milo OS deployment model.

### Migration Path

Because the knowledge service uses `storage.Interface` as the abstraction layer, migration is a two-step operation with no API surface changes:

**Step 1: New storage implementation.**
Implement `storage.Interface` targeting ArangoDB using the official Go driver. The BFS engine in `internal/bfs/` gains an ArangoDB implementation alongside the Postgres one; at query time it issues a single AQL traversal query instead of N batched SQL queries.

**Step 2: One-time reindex.**
All `ResourceRelationship` objects are derived from `RelationshipPolicy` CEL evaluation — they are not user-authored primary data (with the exception of `source.type: Manual` edges). The reindex procedure is:

1. Deprovision the controller-manager's leader to halt new edge writes.
2. Switch the aggregated API server to write to ArangoDB (zero-downtime if done with a rolling update).
3. Restart the controller-manager. On startup, each `RelationshipPolicy` reconciler performs a full re-sync of its watched resources, regenerating all edges in ArangoDB.
4. `Manual` edges must be migrated separately: dump them from Postgres, import into ArangoDB, then verify.
5. Decommission the Postgres edge table (schema tables such as `RelationshipType` remain in the kube-apiserver's etcd).

Estimated reindex duration is bounded by the number of source resources across all tenants and the throughput of the controller-manager's reconcile workers — the same variables that govern initial edge population. No dual-write period is required because edges are fully regenerable from control plane state.

### No API Changes Required

Both `RelationshipType` and `RelationshipPolicy` are CRDs stored in etcd and are not affected by the storage backend migration. `ResourceRelationship` REST API responses are identical regardless of whether the backing store is Postgres or ArangoDB — the `storage.Interface` abstraction ensures callers observe no difference.
