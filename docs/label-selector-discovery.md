# Label-Selector Relationship Discovery — Design Proposal

Status: Proposal
Author: Knowledge service team
Last updated: 2026-05-20

## 1. Problem

`RelationshipPolicy` currently auto-discovers edges via a single CEL expression
that takes the **subject** object as input and returns a list of object refs
(see `pkg/apis/knowledge/v1alpha1/types_relationshippolicy.go` and
`internal/controllers/policy/controller.go`). This is a **push** model: the
subject knows the identity of the object.

That model cannot express the very common **selector** pattern, where the
subject only knows a *predicate* (labels, name-prefix, owner-namespace) and any
object whose metadata satisfies the predicate is an endpoint of the edge.
Concrete cases:

| Subject                | Object              | Selector source                                                                 |
|------------------------|---------------------|---------------------------------------------------------------------------------|
| `Deployment`           | `Pod`               | `spec.selector.matchLabels`                                                     |
| `Service`              | `Pod`               | `spec.selector` (flat map)                                                      |
| `HelmRelease` (Flux)   | any kind            | Labels `helm.toolkit.fluxcd.io/name=<release>`, `helm.toolkit.fluxcd.io/namespace=<ns>` on every managed object |
| `Kyverno ClusterPolicy`| `Namespace`         | `spec.match[].resources.namespaceSelector`                                      |
| `NetworkPolicy`        | `Pod`               | `spec.podSelector.matchLabels` / `matchExpressions`                              |

A pure CEL-over-subject expression cannot enumerate these objects without
materialising every candidate as input — which the current reconciler does via
`listCandidates`, but only in order to *filter* a CEL-produced list. There is
no first-class way to say "the object set is everything that matches this
selector".

This document proposes a `labelSelector` discovery mode that complements the
existing `expression` mode.

## 2. Goals / Non-Goals

**Goals**
- Allow `RelationshipPolicy` to discover objects via label match against a
  per-subject selector.
- Allow the selector itself to be *derived* from the subject (so e.g.
  `Deployment.spec.selector.matchLabels` is the selector for that Deployment).
- Keep the API extensible to non-label predicates later (field selectors,
  namespace selectors) without another breaking change.
- Keep edge lifecycle correct when the **object** changes (label added/removed)
  even though the subject was untouched.

**Non-goals**
- Replacing CEL discovery. CEL stays the default for ref-based edges.
- Building a generic predicate language. We constrain ourselves to standard
  `metav1.LabelSelector` semantics, optionally namespace-scoped.
- Cross-control-plane-context object discovery. Like CEL mode, the object set
  is sourced from a single control plane context per policy.

## 3. API Changes

### 3.1 Shape

We extend `DiscoverySpec` rather than introducing a sibling stanza. The two
modes are mutually exclusive — exactly one of `expression` or `labelSelector`
must be set. This matches the existing field comment ("Exactly ONE field must
be specified") and avoids a duplicate set of `controlPlaneContextRef`,
`relationshipType`, etc.

```go
// DiscoverySpec defines how relationships are discovered from subject resources.
// Exactly ONE of expression or labelSelector must be specified.
// +kubebuilder:validation:XValidation:rule="(has(self.expression) ? 1 : 0) + (has(self.labelSelector) ? 1 : 0) == 1",message="exactly one of expression or labelSelector must be set"
type DiscoverySpec struct {
    // Expression is a CEL expression that receives `subject` (the full
    // Kubernetes object as a map) and returns a list of maps with fields:
    // apiGroup, kind, name, namespace, controlPlaneContextKind,
    // controlPlaneContextName.
    // +optional
    Expression string `json:"expression,omitempty"`

    // LabelSelector enables pull-mode discovery: list every object of the
    // RelationshipType's objectGVK whose labels match a selector derived from
    // (or declared by) the subject.
    // +optional
    LabelSelector *LabelSelectorDiscovery `json:"labelSelector,omitempty"`
}

// LabelSelectorDiscovery configures pull-mode discovery against the
// RelationshipType's objectGVK.
type LabelSelectorDiscovery struct {
    // SelectorExpression is a CEL expression that receives `subject` and
    // returns a map<string,string> of label match requirements (logical AND).
    // The map is interpreted as metav1.LabelSelector.matchLabels.
    //
    // Examples:
    //   subject.spec.selector.matchLabels                                  // Deployment
    //   subject.spec.selector                                              // Service
    //   {"helm.toolkit.fluxcd.io/name": subject.metadata.name,
    //    "helm.toolkit.fluxcd.io/namespace": subject.metadata.namespace}   // HelmRelease
    //
    // Mutually exclusive with MatchLabels/MatchExpressions.
    // +optional
    SelectorExpression string `json:"selectorExpression,omitempty"`

    // MatchLabels is a static label selector used verbatim. Useful only when
    // the selector does not depend on the subject. Mutually exclusive with
    // SelectorExpression.
    // +optional
    MatchLabels map[string]string `json:"matchLabels,omitempty"`

    // MatchExpressions is a static set-based requirement list. Same
    // restrictions as MatchLabels.
    // +optional
    MatchExpressions []metav1.LabelSelectorRequirement `json:"matchExpressions,omitempty"`

    // NamespaceScope controls which namespaces are searched for matching
    // objects. Defaults to SubjectNamespace.
    // +kubebuilder:validation:Enum=SubjectNamespace;AllNamespaces;Explicit
    // +kubebuilder:default=SubjectNamespace
    // +optional
    NamespaceScope string `json:"namespaceScope,omitempty"`

    // Namespaces is consulted only when NamespaceScope=Explicit.
    // +optional
    Namespaces []string `json:"namespaces,omitempty"`

    // MaxMatches caps the number of objects matched for a single subject.
    // If exceeded, the policy goes Ready=False with reason MatchLimitExceeded
    // and no edges are written for that subject (fail-closed).
    // +kubebuilder:default=500
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=10000
    // +optional
    MaxMatches int32 `json:"maxMatches,omitempty"`

    // ObjectControlPlaneContextRef overrides the policy-level context for the
    // discovered object endpoints. Most policies should omit this and inherit
    // spec.controlPlaneContextRef.
    // +optional
    ObjectControlPlaneContextRef *ControlPlaneContextRef `json:"objectControlPlaneContextRef,omitempty"`
}
```

Why this shape:

- A new `labelSelector` field reads naturally (parity with `nodeSelector`,
  `LabelSelector` everywhere else in K8s) and keeps the discovery alternatives
  inside one stanza so a future `fieldSelector` slots in symmetrically.
- A `selectorExpression` (CEL → `map<string,string>`) is the smallest possible
  CEL surface that covers all four motivating cases without forcing users into
  static field paths. Field paths cannot express the Flux case (which composes
  two label keys from `metadata.name`/`namespace`); CEL can.
- We deliberately do **not** introduce a `discovery.mode: Expression|LabelSelector`
  enum — the presence of the field is the discriminator and admission validates
  exclusivity via a CEL `XValidation` rule. Enums add a third source of truth
  that drifts.

### 3.2 No change to `RelationshipType`

`RelationshipType` continues to declare `subjectGVK` and `objectGVK`. The
selector mode resolves objects of `objectGVK` only — there is no cross-kind
selector match. (A future `objectKinds: []GroupVersionKind` could relax this
for Flux's "any kind" use case; see §8 open question 1.)

## 4. Sample Policies

### 4.1 Flux HelmRelease → managed resources (ConfigMap example)

Flux stamps every object it deploys with two labels:
`helm.toolkit.fluxcd.io/name` and `helm.toolkit.fluxcd.io/namespace`. One
policy per object kind is required until we support multi-kind objects (see
open question 1).

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: RelationshipPolicy
metadata:
  name: helmrelease-manages-configmap
  namespace: knowledge-system
spec:
  controlPlaneContextRef:
    kind: Project
    name: my-project
  relationshipType:
    name: helmrelease-manages-configmap   # subjectGVK=HelmRelease, objectGVK=ConfigMap
  subject:
    apiGroup: helm.toolkit.fluxcd.io
    kind: HelmRelease
    namespaces: []
  discovery:
    labelSelector:
      selectorExpression: |
        {
          "helm.toolkit.fluxcd.io/name":      subject.metadata.name,
          "helm.toolkit.fluxcd.io/namespace": subject.metadata.namespace,
        }
      namespaceScope: AllNamespaces
      maxMatches: 2000
```

### 4.2 Deployment → Pod via `spec.selector.matchLabels`

```yaml
apiVersion: knowledge.miloapis.com/v1alpha1
kind: RelationshipPolicy
metadata:
  name: deployment-selects-pod
  namespace: knowledge-system
spec:
  controlPlaneContextRef:
    kind: Project
    name: my-project
  relationshipType:
    name: deployment-selects-pod           # subjectGVK=Deployment, objectGVK=Pod
  subject:
    apiGroup: apps
    kind: Deployment
    namespaces: []
  discovery:
    labelSelector:
      selectorExpression: |
        subject.spec.selector.matchLabels
      namespaceScope: SubjectNamespace
      maxMatches: 1000
```

## 5. Controller Reconciliation Changes

The existing reconciler (`internal/controllers/policy/controller.go`) already
does an O(N·M) sweep — for each subject it lists *all* objects of `objectGVK`
and lets CEL filter them. Label-selector mode reuses the same skeleton but
narrows the candidate fetch and skips CEL filtering.

### 5.1 Reconcile loop

Pseudocode for the label-selector branch inside `reconcileEdges`:

```
for src in sources:
    selector := evalSelectorExpression(policy, src)        // -> map[string]string
    if len(selector) == 0:
        continue                                           // empty selector is a no-op, never "*"
    ns := scopeNamespaces(policy, src)                     // [] = cluster-wide
    matches := listObjectsByLabels(objectGVR, ns, selector, limit=MaxMatches+1)
    if len(matches) > MaxMatches:
        return errMatchLimitExceeded                       // status -> Ready=False, no writes for this src
    for m in matches:
        desired[ deterministicName(policy, src, m) ] = buildRelationship(policy, src, m)
diff desired vs existing-by-label, create/delete
```

Notable rules:

- **Empty selector is fail-closed**: an empty `map<string,string>` does *not*
  match all objects (which would model `Service{}` semantics but creates an
  unbounded edge fanout); it matches nothing. The selector-expression
  evaluator returns an error if the CEL output is `null`, and the CEL
  validator rejects expressions whose statically inferred return type is not
  `map(string, string)`.
- **maxMatches is hard-capped fail-closed**: we keep what was already there
  and don't partially apply. The condition surfaces in status so an operator
  can tune the selector or raise the cap.
- Object identity uses `(apiGroup, kind, namespace, name)` — same as today.
  `deterministicName` already incorporates all of these, so no new collisions.

### 5.2 Watches

Today the reconciler hardcodes a handful of `Watches(&corev1.Service{}, …)` etc.
This is acceptable for CEL mode (the same dynamic re-list every 30s catches
drift). For label-selector mode that periodic resync is much more expensive
because the candidate set is the entire object kind, so we want event-driven
reconciliation.

We add a **dynamic object watch** keyed off the active set of policies. The
manager spins one watch per `(objectGVR, controlPlaneContext)` in the union of
label-selector policies, and reconciles affected policies on object events.

Two implementation options:

| Option | Description | Trade-off |
|---|---|---|
| A. `controller-runtime` dynamic watches via `controller.Watch` from inside the policy reconciler, deduped by GVR | Lazy, minimal churn | Watches outlive policies until manager restart |
| B. A separate `WatchManager` goroutine that reads the `RelationshipPolicyList` and reconciles its informer set on policy add/remove (the pattern used by Kyverno and Crossplane) | Clean lifecycle | More code, leader-elected only |

**Recommendation:** Option B — a tiny `watchSet` struct holding `map[GVR]watch.Interface`, recomputed on `RelationshipPolicy` add/update/delete events, sharing the existing dynamic client. This is consistent with `multicluster-runtime` patterns and gives us a clean place to enforce per-GVR rate limits.

### 5.3 Object → policy mapping

When an object event arrives, we need to enqueue every policy whose selector
*could* match that object. Three strategies, in increasing efficiency:

1. **Naive**: enqueue every label-selector policy whose `objectGVK` matches
   the event's GVK. Acceptable for ≤ ~50 such policies per cluster.
2. **Label-key index**: keep a per-GVR inverted index `labelKey -> []policy`
   built at policy admission time from the (static) `matchLabels` keys; for
   `selectorExpression` policies, we statically extract the set of label
   *keys* the expression projects via a CEL AST walk if possible, otherwise
   fall back to "all policies of this GVR".
3. **Per-subject inverted index**: at reconcile time, cache `selector → []subject`
   so an object event only reconciles subjects whose selector mentions one of
   the object's label keys.

**Recommendation:** ship strategy 1, design the controller code to make
strategy 2 a drop-in upgrade. Strategy 3 is premature.

### 5.4 Watch fan-out guardrails

- A policy whose `selectorExpression` returns the empty map is treated as
  invalid (see §5.1). The static `matchLabels` form is also rejected at
  admission when empty.
- `namespaceScope: AllNamespaces` requires opt-in via an admission
  policy/feature gate at the controller (`--allow-cluster-wide-selectors`).
  We anticipate Flux being the primary user; document the trade-off.
- Per-policy `maxMatches` defaults to 500 (above) but the controller also
  enforces a process-wide cap on the total number of distinct object watches
  (e.g. 25 GVRs), surfaced as a metric.

### 5.5 Reconcile triggers

| Event                                    | What reconciles                                                           |
|------------------------------------------|---------------------------------------------------------------------------|
| Subject add/update/delete                 | The policies that watch that subject GVK (same as today)                  |
| Object add/update                         | All label-selector policies whose `objectGVK` matches (§5.3 strategy 1)   |
| Object label change                       | Same as add/update                                                        |
| Object delete                             | Same as add/update — diff naturally removes the stale edge                |
| `RelationshipPolicy` add/update/delete    | Self + recompute watchSet                                                 |
| Periodic resync                           | Drop to 5 min for label-selector policies (vs 30 s today); informers cover drift |

## 6. Edge Lifecycle

The CEL-mode lifecycle is "subject reconcile → desired set → diff → write".
Label-selector mode follows the same diff-based pattern; the only new wrinkle
is that the *object* can change without the subject changing.

For each policy reconcile, regardless of trigger:

1. List current desired edges: `(subject, selector-matches)` for every subject.
2. List existing edges by the policy's labels (already done).
3. Create missing, delete extra.

Concrete scenarios:

- **Object loses a label**: object watch fires → reconcile the policies whose
  `objectGVK` matches → that object is no longer in `matches` for any subject
  → diff deletes the edge.
- **Object gains a label**: symmetrically, diff creates the edge.
- **Subject changes selector**: subject watch fires → the new selector
  produces a different `matches` set → diff fixes up edges. This already
  works correctly because we always recompute from scratch per subject.
- **Object deleted**: standard delete event → reconcile → object is absent
  from any List → edge removed.
- **Subject deleted**: owner-reference garbage collection deletes the
  `ResourceRelationship` (each edge already has the `RelationshipPolicy` as
  controller owner-ref; no change required).

We deliberately keep state-free reconciliation — no per-subject snapshot
cache. Selector lookups are List calls, not deltas, so each reconcile is
self-correcting after any informer hiccup.

## 7. CEL Selector Extraction

The selector source must accommodate three patterns:

1. **Static field path** (`spec.selector.matchLabels`): a CEL identifier
   chain.
2. **Flat map** (`Service.spec.selector`): also a CEL identifier chain.
3. **Composed labels** (Flux): a CEL map literal with subject-derived values.

A `selectorExpression` returning `map<string,string>` covers all three with
one mechanism:

```cel
// 1.
subject.spec.selector.matchLabels

// 2.
subject.spec.selector

// 3.
{
  "helm.toolkit.fluxcd.io/name":      subject.metadata.name,
  "helm.toolkit.fluxcd.io/namespace": subject.metadata.namespace,
}
```

We considered a `selectorPath` string (e.g. `"spec.selector.matchLabels"`)
for the field-path-only case. Rejected because: (a) it can't model case 3,
forcing two API surfaces; (b) the CEL engine is already wired up; (c) field
paths are no shorter than `subject.<path>` in practice.

### 7.1 Validation

The CEL engine in `internal/cel` is reused unchanged. At admission we type-check
`selectorExpression` and require its inferred result type to be
`map(string, string)`. Anything else is rejected. This is stricter than the
existing CEL-discovery validator (which only checks for compile errors and
shape at runtime) and is enabled by passing a `WithExpectedResultType` option
to the engine — small new method on the CEL package.

The `matchExpressions` support (set-based operators `In`, `NotIn`, `Exists`,
`DoesNotExist`) deliberately is **not** producible from a CEL expression.
Authors wanting set-based selectors use the static `matchExpressions` field.
We will not attempt to round-trip CEL → `LabelSelectorRequirement`.

## 8. Scale Considerations

### 8.1 Object cardinality

A `Service` in a busy namespace can match thousands of Pods. Each match
becomes a `ResourceRelationship` row, which:

- adds a Postgres row written by the aggregated API server,
- is included in any subsequent BFS traversal originating at that Service,
- counts against the policy's `discoveredEdgesCount`.

The graph-traversal limits (`spec.traverse.maxNodes`, `maxDepth`) protect
queriers from blowups but do *not* protect storage. The right boundary is
at **edge creation time**: per-policy `MaxMatches` (above). Reusing
`maxNodes` would conflate two concerns and would not gate writes.

We also propose a soft warning condition `EdgeCountHigh` on the policy when
`discoveredEdgesCount > 0.5 * sum(MaxMatches across subjects)`.

### 8.2 Watch cardinality

The label-selector branch demands one informer per distinct `objectGVR` in
the active policy set. To bound this:

- A controller flag `--max-label-selector-gvrs` (default 25).
- A policy whose objectGVR pushes us over the limit goes Ready=False with
  `WatchBudgetExceeded`.
- The flag is surfaced via a `knowledge_policy_object_watches` Prometheus
  gauge.

### 8.3 Reconcile fanout

When an object label changes, strategy 1 reconciles every label-selector
policy of that objectGVK. For a 1000-pod / 50-deployment cluster this is
50 reconciles per pod label flip. Each reconcile is bounded by:

- a single List of subjects (cached, controller-runtime informer),
- one List per subject against the object kind (label-selected, so server-side
  filtered).

We mitigate hot loops by adding `workqueue.DefaultControllerRateLimiter()` to
the per-policy queue (already implied by `ctrl.NewControllerManagedBy`).

### 8.4 Comparison to current CEL mode

| Concern              | CEL mode                       | Label-selector mode                       |
|----------------------|--------------------------------|-------------------------------------------|
| Candidate fetch      | Full List of objectGVK         | Server-side label-selected List           |
| Per-subject cost     | O(M) (M = all objects)         | O(matches)                                |
| Object-change reactivity | Periodic 30s requeue       | Event-driven via dynamic watch            |
| Memory               | Holds full candidate set in CEL eval | Holds only matches                |

Label-selector mode is **cheaper** per subject when selectors are tight, but
introduces the watch budget concern above.

## 9. Status & Conditions

New conditions emitted on the policy:

| Condition                  | When                                                      |
|----------------------------|-----------------------------------------------------------|
| `SelectorValid=False`      | `selectorExpression` returned non-map, empty map, or compile error |
| `MatchLimitExceeded=True`  | Any subject hit `MaxMatches`                              |
| `WatchBudgetExceeded=True` | Controller refused to spin up the object watch            |
| `EdgeCountHigh=True`       | Soft warning at 50% of capacity                           |

The existing `Ready` condition stays as the rollup signal.

## 10. Migration & Backwards Compatibility

- `DiscoverySpec.Expression` becomes `+optional` (was effectively required).
  CEL admission gate is the new `XValidation` rule.
- Existing policies continue to work unchanged.
- The CRD bump is a minor API change within `v1alpha1`; no version conversion
  required.
- No `ResourceRelationship` schema changes.

## 11. Open Questions

1. **Multi-kind object discovery for Flux.**
   Today a `RelationshipType` declares a single `objectGVK`. Flux's
   HelmRelease manages *every* kind of resource it produces. Modeling it
   today requires N RelationshipTypes (one per managed kind).
   **Recommendation:** keep the single-objectGVK constraint for v1 and ship a
   separate RFC for `objectGVKs: []GVK` (which affects RelationshipType,
   storage indexing, and BFS). HelmRelease users author one policy per kind
   they care about (typically Deployment, Service, ConfigMap, Secret) — that
   covers ~95% of the value.

2. **Cluster-wide vs subject-namespace defaults.**
   `Service.spec.selector` is namespace-scoped (matches pods only in the
   same namespace). `HelmRelease` is cluster-wide. Defaulting
   `namespaceScope` to `SubjectNamespace` is the safer default; Flux users
   opt into `AllNamespaces`.
   **Recommendation:** keep `SubjectNamespace` default and require explicit
   `AllNamespaces`, gated by the controller flag from §5.4.

3. **Should `selectorExpression` support set-based requirements?**
   E.g. returning `[{key, operator, values}…]` instead of a flat map.
   **Recommendation:** no. Set-based requirements are rare in the
   motivating cases. Authors who need them use the static
   `matchExpressions` field. Keeps the CEL contract simple
   (`map<string,string>` only).

4. **What happens when the selector references a missing subject field?**
   E.g. `subject.spec.selector.matchLabels` on a Service that has only
   `spec.selector` (flat). CEL throws a no-such-key error today.
   **Recommendation:** treat any CEL evaluation error as
   `SelectorValid=False` *for that subject* (not the whole policy) — skip
   the subject, count the failures, surface the first error in the
   condition message. Mirrors how a CEL discovery error today aborts the
   whole reconcile, which we should also relax.

5. **Should we deduplicate edges across modes?**
   A user could write *both* a CEL policy and a label-selector policy that
   produce the same `(subject, object)` pair. Today `deterministicName`
   includes the policy name, so they would create two distinct
   `ResourceRelationship` rows.
   **Recommendation:** leave as-is. Two policies = two edges is consistent
   with the current contract and lets each policy be deleted independently.
   A future "canonical edge" view can dedupe at query time.

## 12. Out of Scope (Follow-ups)

- Field-selector discovery (e.g. match by `spec.nodeName`).
- Multi-kind objects (`objectGVKs`).
- Cross-control-plane-context selectors.
- Selector → BFS shortcut: today every selector match is materialised as a
  row. Tomorrow we may want a *virtual* edge representation (`source → selector`
  evaluated at query time) to avoid storing millions of Service→Pod rows.
  This requires storage and traversal changes and is its own design.
