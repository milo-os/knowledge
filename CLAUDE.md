# Knowledge Graph Service

This repository implements the Milo OS knowledge graph service — a typed resource relationship system that goes beyond Kubernetes `ownerReferences`. See `docs/design.md` for the full design.

## What This Service Does

Stores typed edges between Kubernetes resources (e.g., `Domain AttachedTo Gateway`, `User Member Organization`) and exposes a graph traversal API. Relationships are auto-discovered by `RelationshipPolicy` controllers that evaluate CEL expressions against watched resources.

## API Group: `knowledge.miloapis.com/v1alpha1`

Four resource types:

| Type | Kind | Notes |
|---|---|---|
| CRD | `RelationshipType` | Cluster-scoped. Declares a relationship type schema. |
| CRD | `RelationshipPolicy` | Namespaced. CEL expression auto-discovery rule. |
| CRD | `ResourceRelationship` | Namespaced. Instantiated edge, persisted in Postgres. |
| Aggregated API | `GraphQuery` | Create-only (SubjectAccessReview pattern). Synchronous BFS traversal. |

## Monorepo Context

This service lives alongside other Milo OS services at `/Users/scotwells/repos/milo-os/`. The closest analogues are:

- **`../ipam/`** — Postgres-backed aggregated API server. Follow its `internal/storage/postgres/` and `internal/watch/postgres.go` patterns exactly.
- **`../automation/`** — Tenant-scoped Postgres storage (`cluster_id` column). Follow `internal/apiserver/storage/postgres/store.go`.
- **`../milo/`** — Core platform. Key reference files:
  - `internal/apiserver/identity/serviceaccountkeys/rest.go` — create-only REST handler (the SAR pattern for `GraphQuery`)
  - `vendor/k8s.io/kubernetes/pkg/registry/authorization/subjectaccessreview/rest.go` — canonical SAR implementation
  - `internal/apiserver/storage/identity/storageprovider.go` — StorageProvider wiring
  - `cmd/milo/apiserver/config.go` — GenericStorageProviders registration
  - `internal/quota/engine/cel.go` — CEL evaluation with cost limiting (use for RelationshipPolicy)
  - `internal/controllers/garbagecollector/graph.go` — graph node/edge structure reference
  - `pkg/server/discovery/contexts.go` — ParentContext enum (Platform/Organization/Project/User) used in ControlPlaneContextRef
  - `internal/quota/controllers/policy/parent_context_resolver.go` — cross-context client resolution

## Key Architecture Decisions

**Edge storage:** `ResourceRelationship` CRDs persisted in Postgres via the aggregated API server's `storage.Interface`. Same `pgx/v5` pattern as `ipam` and `automation`. Backend is swappable (ArangoDB is the preferred future target).

**Graph traversal:** Application-level BFS — one batched SQL query per depth level (`WHERE source_id = ANY($1)`). No recursive CTEs.

**GraphQuery:** Synchronous, create-only (SubjectAccessReview pattern). POST spec, result returned immediately in the HTTP response. Enforces `spec.traverse.maxDepth` and `spec.traverse.maxNodes`.

**Cross-context refs:** Single `ResourceRelationship` type with mandatory `ControlPlaneContextRef{Kind, Name}` on each endpoint. `Kind` follows the `ParentContext` enum: `Platform | Organization | Project | User`.

**LCA storage rule:** `ResourceRelationship` objects are stored at the lowest common ancestor control plane context. Same-org cross-project edges go to the Organization context (not Platform) to prevent platform-wide watch fanout.

**AuthZ:** No OpenFGA calls during traversal. Control plane context routing already scopes the traversal to what the caller can access — the same boundary as any other Kubernetes resource.

**RelationshipPolicy discovery:** Single CEL `expression` field only. The expression receives `subject` (the full Kubernetes object) and returns a list of object refs. No shorthand field variants.

## Development Commands

```bash
# Generate deepcopy, CRDs
go generate ./...

# Build
go build ./cmd/knowledge/apiserver/...
go build ./cmd/knowledge/controller-manager/...

# Run unit tests
go test ./...

# End-to-end tests (requires kind cluster)
cd test/kind && ./setup.sh
chainsaw test ../e2e/chainsaw/
```

## Repository Structure

```
knowledge/
├── cmd/knowledge/
│   ├── apiserver/          # Aggregated API server binary
│   └── controller-manager/ # Controller manager binary
├── pkg/apis/knowledge/v1alpha1/
│   ├── types_relationshiptype.go
│   ├── types_relationshippolicy.go
│   ├── types_resourcerelationship.go
│   ├── types_graphquery.go
│   ├── zz_generated.deepcopy.go
│   └── register.go
├── internal/
│   ├── apiserver/
│   │   ├── storage/postgres/ # storage.Interface backed by pgx/v5
│   │   └── graphquery/       # create-only REST handler
│   ├── controllers/
│   │   ├── policy/           # RelationshipPolicy reconciler (leader-elected)
│   │   └── relationship/     # ResourceRelationship validity controller
│   └── bfs/                  # BFS traversal engine
├── config/
│   ├── crd/                  # Generated CRD manifests
│   ├── apiserver/
│   ├── controller-manager/
│   └── samples/
├── docs/
│   ├── design.md             # Full design document
│   └── implementation-team-prompt.md # Multi-agent implementation brief
└── test/
    ├── e2e/chainsaw/         # Chainsaw test suites
    └── kind/                 # Kind cluster config + setup script
```

## Go Module

Module: `go.miloapis.com/knowledge`

Match dependency versions from `../milo/go.mod` for:
- `k8s.io/apiserver`
- `k8s.io/controller-runtime`
- `k8s.io/apimachinery`
- `github.com/jackc/pgx/v5`
- `sigs.k8s.io/multicluster-runtime`
