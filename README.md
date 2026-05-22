# Knowledge Graph Service

A typed resource relationship layer for Kubernetes-native platforms — built for [Milo OS](https://github.com/milo-os).

---

## The Problem

Kubernetes `ownerReferences` are the built-in way to relate resources. But they're ownership-only, single-namespace, and have no graph traversal API. They can't answer questions like:

- Which Gateway does this Domain attach to?
- Which Users are members of this Organization?
- What certificates secure this service's ingress chain?
- Trace every resource that depends on this database — across projects.

Answering these today means writing bespoke controllers that each maintain their own relationship stores, with no consistent way to query across them.

## The Solution

The Knowledge Graph Service adds a **typed relationship layer** that sits alongside the existing ownership graph. It provides:

- **Typed, directed edges** between any two resources, across namespaces, projects, or organizations.
- **Policy-driven auto-discovery** — define a CEL expression, and edges are automatically created and removed as resources change.
- **Synchronous graph traversal** via a single `GraphQuery` API call.
- **Consistent storage** backed by PostgreSQL, with a clear migration path to a dedicated graph database.

---

## How It Works

### 1. Declare relationship types

`RelationshipType` is a cluster-scoped schema that defines the semantics of an edge:

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: RelationshipType
metadata:
  name: domain-attached-to-gateway
spec:
  displayName: AttachedTo
  description: Ingress domain is attached to a Gateway
  subjectGVK:
    group: networking.k8s.io
    version: v1
    kind: Ingress
  objectGVK:
    group: gateway.networking.k8s.io
    version: v1
    kind: Gateway
  cardinality: ManyToMany
```

### 2. Auto-discover relationships with CEL

`RelationshipPolicy` watches a resource kind and runs a CEL expression to derive edges. The policy controller creates and removes `ResourceRelationship` objects automatically as resources change:

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: RelationshipPolicy
metadata:
  name: certificate-issued-by-clusterissuer-policy
  namespace: knowledge-system
spec:
  controlPlaneContextRef:
    kind: Platform
    name: local
  relationshipType:
    name: certificate-issued-by-clusterissuer
  subject:
    apiGroup: cert-manager.io
    kind: Certificate
  discovery:
    expression: |
      "issuerRef" in subject.spec && subject.spec.issuerRef.kind == "ClusterIssuer" ?
        [{"apiGroup": "cert-manager.io", "kind": "ClusterIssuer", "name": subject.spec.issuerRef.name}] :
        []
```

No controller code required. If the cert changes its issuer, the edge updates automatically.

### 3. Query the graph

`GraphQuery` is a synchronous, create-only API (SubjectAccessReview pattern) that runs a bounded BFS traversal from any starting resource:

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: GraphQuery
metadata:
  generateName: trace-
spec:
  root:
    apiGroup: gateway.networking.k8s.io
    kind: Gateway
    name: prod-gateway
    namespace: infra
  traverse:
    maxDepth: 4
    maxNodes: 200
```

The response returns all reachable nodes and edges within the specified bounds — scoped to what the requesting principal can access, with no extra authorization overhead.

---

## API Reference

| Resource | Kind | Scope | Purpose |
|---|---|---|---|
| CRD | `RelationshipType` | Cluster | Declares a typed edge schema |
| CRD | `RelationshipPolicy` | Namespaced | CEL expression auto-discovery rule |
| CRD | `ResourceRelationship` | Namespaced | A single persisted directed edge |
| Aggregated API | `GraphQuery` | Namespaced | Synchronous BFS traversal |

API group: `knowledge.miloapis.com/v1alpha1`

---

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    Milo OS Control Plane                  │
│                                                          │
│  ┌───────────────────────────────────────────────────┐   │
│  │            Aggregated API Server                  │   │
│  │  RelationshipType  (CRD, cluster-scoped)          │   │
│  │  RelationshipPolicy (CRD, namespaced)             │   │
│  │  ResourceRelationship ──────────────────────┐     │   │
│  │  GraphQuery (create-only, SAR pattern)       │     │   │
│  └──────────────────────────────────────────────┼─────┘   │
│                                                 │         │
│  ┌──────────────────────┐         ┌─────────────▼──────┐  │
│  │   Controller Manager │         │    PostgreSQL       │  │
│  │                      │  writes │                    │  │
│  │  RelationshipPolicy  ├────────►│  resource_          │  │
│  │  reconciler          │         │  relationships      │  │
│  └──────────────────────┘         └────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

**Key design decisions:**

- **Application-level BFS** — one batched SQL query per depth level, no recursive CTEs.
- **LCA storage rule** — cross-project edges are stored at the Organization context, not Platform, to avoid platform-wide watch fanout.
- **No OpenFGA per traversal** — control plane context routing already scopes the traversal to what the caller can access.
- **Swappable backend** — ArangoDB is the preferred long-term target; the `storage.Interface` abstraction makes migration incremental.

---

## Development

```bash
# Generate deepcopy functions and CRD manifests
go generate ./...

# Build
go build -o knowledge ./cmd/knowledge

# Unit tests
go test ./...

# End-to-end tests (requires kind cluster)
cd test/kind && ./setup.sh
chainsaw test ../e2e/chainsaw/
```

---

## Status

`v1alpha1` — active development. API types are stable enough for experimentation but subject to change before v1beta1.

## Related

- [Milo OS](https://github.com/milo-os) — the platform this service is part of
- [Design Document](docs/design.md) — full architecture and rationale
- [datum-cloud/datum#164](https://github.com/datum-cloud/datum/discussions/164) — the original discussion that motivated this work
