# Knowledge Graph UI — Consolidated Specification

## 1. Overview

This document is the single, authoritative implementation spec for the Milo OS Knowledge Graph UI. It synthesizes the UX spec (`ux-spec.md`), frontend architecture (`frontend-spec.md`), and backend gap analysis (`backend-gap-analysis.md`) into one buildable plan covering the five designed screens. The UI is a React 18 + TypeScript SPA talking directly to the aggregated `knowledge.miloapis.com/v1alpha1` API server. The system under specification is the user-facing surface for exploring, querying, and managing typed resource relationships across Platform, Organization, Project, and User control plane contexts.

---

## 2. App Shell & Routing

The Pencil design uses **two distinct shells**:

- **Sidebar shell** (Screens 1–3): 260px left filter sidebar + canvas. Graph-centric workflow.
- **Top-tab shell** (Screens 4–5): 56px horizontal nav bar with three tabs (Graph Explorer / Relationships / Types & Policies). Table/catalog workflow.

**Reconciliation strategy:** one `<AppLayout>` with both chrome elements; the sidebar collapses to icon-only (or unmounts) on `/relationships` and `/catalog`, and the top-tab strip is always present. The "Graph Explorer" top tab links to `/graph`, where the sidebar re-expands. This avoids two parallel layouts and keeps cross-screen navigation in one place. (See §7 question 1 — alternative is fully separate routes with no shared shell.)

### Route table

| Path | Screen | Overlays via search params |
|---|---|---|
| `/` | redirect → `/graph` | — |
| `/graph` | Screen 1 — Graph Explorer | `?node=<kind>/<ns>/<name>` opens Drawer (Screen 2); `?query=open` opens Query Builder (Screen 3) |
| `/relationships` | Screen 4 — Relationship Inventory | `?type=&kind=&source=&validity=&context=&q=&page=<continueToken>` |
| `/catalog` | Screen 5 — Types & Policies | `#types` (default) / `#policies` |
| `/catalog/policies/new` | Create RelationshipPolicy modal route | — |

URL-driven overlays keep the graph canvas mount stable when opening/closing the drawer (no canvas re-init cost) and make every view deep-linkable.

---

## 3. Per-Screen Implementation Plan

### Screen 1 — Graph Explorer (`/graph`)

**Purpose:** Filterable node-link view of the knowledge graph, banded horizontally by control plane context.

**Component tree**
```
<GraphExplorerPage>
 ├ <GraphSidebar>                  // 260px filter rail
 │  ├ <ContextSelect/>             // Platform | Organization | Project | User
 │  ├ <RelationshipTypeCheckboxes/>
 │  ├ <DirectionSegmentedControl/> // Outbound | Inbound | Both
 │  └ <RunQueryButton/>            // opens ?query=open
 ├ <GraphMain>
 │  ├ <GraphTopBar/>               // breadcrumb + global search
 │  ├ <GraphCanvas>                // react-force-graph-2d
 │  │  ├ <ContextBands/>           // SVG overlay, polygon hulls per context
 │  │  ├ <GraphNode/>              // nodeCanvasObject custom renderer
 │  │  └ <GraphEdge/>              // linkCanvasObject + edge pill
 │  └ <TruncationBanner/>          // shows when last response.status.truncated
 ├ <NodeDetailDrawer/>             // mounted when ?node=
 └ <QueryBuilderModal/>            // mounted when ?query=open
```

**API calls**
- `POST /apis/knowledge.miloapis.com/v1alpha1/namespaces/{ns}/graphqueries` — submit `GraphQuery` spec; result is in response body.
- `GET /apis/.../relationshiptypes` — populate sidebar checkbox list.

**State**
- React Query: `useMutation` for `submitGraphQuery`; cached under `['graphquery', hash(spec)]` so navigating back replays the last result without re-POSTing.
- URL: `?node=`, `?query=open`.
- Zustand: `lastQuerySpec`, `showContextBands`, `layout` toggle.

**Key interactions**
- Sidebar filters re-filter the **already-loaded** graph client-side (no refetch). This matches the UX finding that sidebar = live filter, modal = fresh query.
- Run Query button opens Screen 3 modal pre-filled from `lastQuerySpec`.
- Node click → set `?node=<kind>/<ns>/<name>`, drawer mounts.
- Edge pill click → navigate to `/relationships?type=<edgeType>`.
- Top-bar search → resource picker; on select, opens Query Builder with that root.
- Context band placement keys off each node's `endpoint.controlPlaneContextRef.Kind`.

**Backend gaps affecting this screen**
- Gap #3: `truncated` flag fires only on `maxNodes`, not `maxDepth` — UI must detect depth-cap client-side (any node at `maxDepth` with unexplored neighbors implies truncation).
- Gap #7: banner copy must read the submitted `maxNodes` value, not hardcoded "500".
- Gap #9: no server-side history — implement client-side `lastQuerySpec` history in localStorage.

---

### Screen 2 — Node Detail Drawer (overlay on `/graph`)

**Purpose:** Right-side drawer showing identity + inbound/outbound relationships of the selected node.

**Component tree**
```
<NodeDetailDrawer>                 // Radix Dialog, side-drawer variant
 ├ <DrawerHeader/>                 // "Resource Detail" + close
 ├ <IdentityBlock/>                // Kind chip, Name, Namespace, Context pill, APIVersion
 ├ <DirectionTabs>                 // Outbound | Inbound
 │  └ <RelationshipList>
 │     └ <RelationshipRow/>        // type chip + target + source badge + Traverse →
 └ <DrawerFooter/>                 // (future) "Center in graph"
```

**API calls**
- No additional fetch in the happy path — the parent graph response already contains all edges incident to the selected node. UI filters `links` locally.
- Fallback (deep-link to a node not in current graph): `POST graphqueries` with `root=<node>`, `maxDepth=1`, `direction=Both`.

**State**
- URL: `?node=<kind>/<ns>/<name>`, `?dir=outbound|inbound` (default outbound).
- React Query: shared with Screen 1's GraphQuery result.

**Key interactions**
- Tab toggle Outbound ↔ Inbound: updates `?dir=`; filters the same `links` array by direction.
- Row "Traverse →" → updates `?node=` to the peer endpoint.
- Backdrop click / X / Esc → clears `?node=`.

**Backend gaps affecting this screen**
- Gap #1 (RESOLVED): the Metadata tab requires labels/annotations/conditions of the referenced resource. The UI has access to a control plane resource fetcher — use the node's `ControlPlaneContextRef` to route a GET to the appropriate control plane API for that `Kind/Namespace/Name`. The `<MetadataTab>` component should render labels, annotations, `creationTimestamp`, owner references, and status conditions fetched from the native control plane.

---

### Screen 3 — Graph Query Builder (modal overlay on `/graph`)

**Purpose:** Compose and submit a `GraphQuery` spec.

**Component tree**
```
<QueryBuilderModal>                // Radix Dialog
 ├ <TruncationBanner/>             // conditional, from previous run
 ├ <QueryBuilderForm>              // react-hook-form + zod
 │  ├ <RootResourceField/>         // resource picker → spec.root
 │  ├ <RelationshipTypeCheckboxes/>
 │  ├ <DirectionRadioGroup/>
 │  ├ <MaxDepthSlider/>            // 1–10 in UI (server allows up to 20)
 │  └ <MaxNodesSlider/>            // 50–1000, default 500
 └ <ModalFooter/>                  // Run Query
```

**Spec mapping (1:1 with `GraphQuerySpec`)**
- Root → `spec.root.{apiGroup, kind, name, namespace, controlPlaneContextRef.{kind, name}}`
- Relationship type checkboxes → `spec.traverse.relationshipTypes[]`
- Direction radio → `spec.traverse.direction` (`Outbound|Inbound|Both`)
- Max Depth slider → `spec.traverse.maxDepth` (1–10 UI; server cap 20)
- Max Nodes slider → `spec.traverse.maxNodes` (50–1000; default 500 in UI, 100 server default)

**API calls**
- `GET /apis/.../relationshiptypes` to populate Kind options for the root picker (each RelationshipType lists `subjectGVK`/`objectGVK`).
- `POST .../graphqueries` on submit.
- Control plane context list comes from the Milo platform API (out of scope for this service).

**State**
- URL: `?query=open` controls modal visibility.
- react-hook-form local form state, validated by zod (depth ∈ [1,10], nodes ∈ [50,1000]).
- Zustand: `lastQuerySpec` pre-fills on open.

**Key interactions**
- All controls update form state immediately (no Apply step).
- Run Query → mutation → on success, write result into React Query cache under `['graphquery', hash(spec)]`, set `lastQuerySpec`, close modal.
- Truncation banner is shown if the **previous** result has `status.truncated === true`.

**Backend gaps affecting this screen**
- Gap #4: no enumeration of valid Kinds / ControlPlaneContextRefs — derive Kind options from `RelationshipType` list; ControlPlaneContext list from Milo platform API.

---

### Screen 4 — Relationship Inventory (`/relationships`)

**Purpose:** Tabular browse of all `ResourceRelationship` instances with filters and pagination.

**Component tree**
```
<RelationshipInventoryPage>
 ├ <PageHeader/>                   // title, Export, Create Relationship (deferred)
 ├ <InventoryFilters>
 │  ├ <SearchInput/>
 │  ├ <ContextFilter/>
 │  ├ <TypeFilter/>
 │  ├ <SourceFilter/>
 │  └ <ValidityFilter/>
 ├ <RelationshipTable>
 │  └ <RelationshipRow/>           // type pill / From / To / Source / Valid / Created
 ├ <TableFooter>
 │  ├ <ResultCount/>                // "Showing 1–N of M+"
 │  └ <Pagination/>                 // k8s continue-token cursors
```

**API calls**
- `GET /apis/.../namespaces/{ns}/resourcerelationships?limit=50&continue=<token>` (or all-namespaces).
- `GET /apis/.../resourcerelationships?watch=true&resourceVersion=N` — streaming watch via `useWatch`.
- `DELETE .../resourcerelationships/{name}` on row delete.

**State**
- URL: `?type=&kind=&source=&validity=&context=&q=&page=<continueToken>`.
- React Query: `['knowledge', 'v1alpha1', 'resourcerelationships', listOpts]`; updated incrementally by `useWatch`.
- Zustand: none for this screen.

**Key interactions**
- Filter changes → write to URL, refetch with new label selector(s).
- Row click → opens detail drawer (reuses Screen 2 component) or navigates to the relationship's detail.
- Column-header click → client-side sort within current page (k8s has no server-side sort).
- Export → downloads currently filtered set as JSON.
- Pagination → updates `?page=<continueToken>`.

**Backend gaps affecting this screen**
- Gap #2 (RESOLVED): filter labels `knowledge.miloapis.com/relationship-type`, `/subject-kind`, `/source-type` are stamped server-side in `resourceRelationshipStrategy`. UI passes `?labelSelector=knowledge.miloapis.com/relationship-type=<name>` (and/or others) on LIST. Multiple filters are ANDed via comma-separated label selector.
- Gap #5: no server-side sort → sort client-side within the loaded page; document the limitation in the column header tooltip.
- Gap #8: no exact total count → use `metadata.remainingItemCount + items.length`; UI copy reads "Showing 1–N of M+" rather than a hard "247 relationships".

---

### Screen 5 — Types & Policies Catalog (`/catalog`)

**Purpose:** Browse `RelationshipType` schemas and `RelationshipPolicy` discovery rules.

**Component tree**
```
<CatalogPage>
 ├ <PageHeader/>                   // title + "Create Type" button (opens modal)
 ├ <CatalogSubnav/>                // Relationship Types | Discovery Policies
 ├ <RelationshipTypesGrid>         // when #types
 │  └ <TypeCard/>                  // name, edge-count, cardinality/directedness, from→to kinds
 └ <PoliciesGrid>                  // when #policies
    ├ <CreatePolicyButton/>        // → /catalog/policies/new
    └ <PolicyCard/>                // name, target RelationshipType, subject Kind, discovered count, status
<PolicyCreateModal>                // at /catalog/policies/new
<CreateRelationshipTypeModal>      // Radix Dialog, opened by "Create Type" button
 ├ <form> (react-hook-form + zod)
 │  ├ name input                   // metadata.name, required, k8s name constraints
 │  ├ displayName input            // spec.displayName, optional
 │  ├ description textarea         // spec.description, optional
 │  ├ <GVKFields label="Subject"/> // spec.subjectGVK — group/version/kind text inputs
 │  ├ <GVKFields label="Object"/>  // spec.objectGVK — group/version/kind text inputs
 │  └ cardinality <Select>         // OneToOne | OneToMany | ManyToMany
 └ <ModalFooter/>                  // Cancel + Create
```

**API calls**
- `GET /apis/.../relationshiptypes`
- `GET /apis/.../namespaces/{ns}/relationshippolicies`
- Watch on both.
- `POST .../relationshippolicies` (form: name, namespace, relationshipType, subject.Kind, CEL expression).
- `DELETE .../relationshippolicies/{name}`.

**State**
- URL: hash (`#types` / `#policies`).
- React Query for both lists; live via `useWatch`.
- Per-type edge counts: lazy fetch by issuing a count-only LIST with `?limit=1` and reading `remainingItemCount + 1`; cached for 60s. (Alternative: rely on `RelationshipPolicy.status.discoveredEdgesCount` for policy-driven types.)

**Key interactions**
- Card click → expanded detail (sample edges + producing policies).
- Edge-count chip click → navigates to `/relationships?type=<thisType>`.
- Create Policy → modal at `/catalog/policies/new` with CEL editor + zod-validated form; on submit, poll `status.conditions` for compile errors (no dry-run endpoint).

**API calls (additions)**
- `POST /apis/knowledge.miloapis.com/v1alpha1/relationshiptypes` — create type (cluster-scoped, no namespace); on success invalidate the types list cache and show toast.
- `DELETE /apis/knowledge.miloapis.com/v1alpha1/relationshiptypes/{name}` — delete type.

**Backend gaps affecting this screen**
- Gap #6: no CEL dry-run — UI polls `status.conditions` on the newly created policy and surfaces compile/eval errors.
- Gap #10 (RESOLVED): `<CreateRelationshipTypeModal>` implements a functional create form. Backend CREATE is fully supported today.
- The design does not specify what the Discovery Policies grid looks like. Recommendation: mirror the TypeCard layout (name, target type, subject Kind, `status.discoveredEdgesCount`, conditions summary). **Decision needed** (see §7).

---

## 4. Backend Gaps & Decisions Required

| # | Gap | Severity | Screen(s) | Recommendation | Status |
|---|---|---|---|---|---|
| 1 | Metadata tab needs resource content not stored in the knowledge graph | ~~Blocking~~ | 2 | Fetch resource via control plane client using `ControlPlaneContextRef` + Kind/Namespace/Name | Resolved in spec |
| 2 | No server-side filter on `relationshipType`/`subject.kind`/`source.type` | High | 4 | Label-stamp via `resourceRelationshipStrategy.PrepareForCreate/PrepareForUpdate` (apiserver-side); UI filters via `?labelSelector=knowledge.miloapis.com/relationship-type=X` | Resolved in spec |
| 3 | `truncated=true` not set on maxDepth cap | Medium | 1 | Detect client-side; backend ticket to fix `bfs/bfs.go` | Resolved in spec (client detection) |
| 4 | No enumeration endpoint for Kinds / ControlPlaneContextRefs | Medium | 3 | Derive Kinds from `RelationshipType` list; pull contexts from Milo platform API | Resolved in spec |
| 5 | No server-side sort | Medium | 4 | Sort client-side within the current page; document | Resolved in spec |
| 6 | No CEL expression dry-run | Medium | 5 | Poll `status.conditions` after CREATE; surface errors | Resolved in spec |
| 7 | Truncation banner copy hardcodes "500 node limit"; server cap is 1000 | Low | 1, 3 | Render the user's submitted `maxNodes` value in the banner | Resolved in spec |
| 8 | No exact total count from k8s LIST | Low | 4 | Use `items.length + remainingItemCount`; copy is "N of M+" | Resolved in spec |
| 9 | No server-side GraphQuery history | Low | 1 | Client-side recent queries in localStorage | Resolved in spec |
| 10 | RelationshipType create not in design | Low | 5 | Implement `<CreateRelationshipTypeModal>` — functional create form to demonstrate the feature | Resolved in spec |
| 11 (new) | Selected-node glow color `#A78BFA44` is off-palette | Low | 1 | Add `--node-selected-glow` token | Resolved in spec |
| 12 (new) | Discovery Policies grid layout unspecified in design | Medium | 5 | Mirror TypeCard layout with policy-specific fields | **Decision needed** |
| 13 (new) | Two distinct app shells (sidebar vs. top-tab) not stitched | Medium | all | Single `<AppLayout>` with conditional sidebar; top-tab always present | **Decision needed** (confirm preferred reconciliation) |
| 14 (new) | Aggregated API CORS not configured for browser SPA | Medium | all | Same-origin reverse proxy via `kubectl proxy` (dev) and in-cluster ServiceAccount token (prod) — no CORS config needed | Resolved in spec |
| 15 (new) | OIDC client registration for browser app | Medium | all | No OIDC — UI is internal/testing only. Dev: kubectl proxy + client cert. Prod: ServiceAccount token mounted in pod | Resolved in spec |

### Decisions needed (questions for the user)

1. **App shell reconciliation:** confirm the single `<AppLayout>` (top-tab always visible, sidebar collapses on non-graph routes) — or do you want two fully separate layouts?
2. **Discovery Policies grid:** OK to ship a PolicyCard mirroring TypeCard (name, target RelationshipType, subject Kind, `discoveredEdgesCount`, status indicator)?
3. **Create Relationship (Screen 4):** defer to post-MVP, or build a minimal manual-source form now?

---

## 5. Frontend Architecture Summary

Full detail in `frontend-spec.md`. Key choices:

- **Stack:** React 18 + TypeScript (strict) + Vite 5 + pnpm. Node 20 LTS. Vitest + Playwright.
- **Routing:** `react-router-dom` v6. Drawer + Query Builder modal are URL-driven overlays (search params), not nested routes — keeps the graph canvas mount stable.
- **State (three layers, do not mix):**
  - Server: TanStack Query v5. Query keys mirror the k8s URL shape. Mutations for `GraphQuery` POST and `RelationshipPolicy` create/delete.
  - URL: react-router search params for selection, filters, pagination cursor, overlay open/closed.
  - UI globals: a single Zustand store (`currentContext`, `lastQuerySpec`, layout toggles, toasts). Auth identity in a separate `AuthContext`.
- **API client:** typed resource modules under `src/api/resources/`. Types generated from the aggregated server's OpenAPI v3 via `openapi-typescript`. Pagination uses k8s `limit` + `continue` tokens, total count derived from `remainingItemCount`.
- **Watch:** streaming `fetch` over `?watch=true&resourceVersion=…`; normalized into React Query cache by a `useWatch` hook. Used on Screens 4 and 5. Screen 1 graph canvas is a point-in-time snapshot — no watch.
- **Graph:** `react-force-graph-2d` with custom `nodeCanvasObject`/`linkCanvasObject` renderers. Context bands drawn as an SVG overlay with `d3-polygon.polygonHull`. A pure mapper `mapGraphQueryResultToCanvasModel` converts the API response to `{nodes, links}`.
- **Auth:** ServiceAccount token (`/var/run/secrets/kubernetes.io/serviceaccount/token`) in-cluster; kubeconfig client cert proxied via `vite dev server` → `kubectl proxy` for local dev. No OIDC. This UI is internal/testing only.
- **Styling:** hand-rolled CSS Modules + CSS custom properties from `src/styles/tokens.css`. No Tailwind. Dark-mode-only for v1.
- **File layout:** `ui/` at repo root with `src/api`, `src/screens/{graph,nodeDetail,queryBuilder,inventory,catalog}`, `src/components`, `src/layout`, `src/state`, `src/hooks`, `src/styles`. Full tree in `frontend-spec.md` §9.

---

## 6. Design Token Inventory

CSS custom properties on `:root`, defined in `src/styles/tokens.css`. Values come from the Pencil `get_variables` output; names below are the contract.

### Backgrounds
| Token | Pencil value |
|---|---|
| `--bg-base` | `#FFFFFF` |
| `--bg-card` | `#FFFFFF` |
| `--bg-surface` | `#F6F6F5` |
| `--bg-sidebar` | `#F6F6F5` |
| `--bg-elevated` | `#EFEFED` |

### Borders
| Token | Pencil value |
|---|---|
| `--border` | `#E8E7E4` |
| `--border-subtle` | `#EFEFED` |

### Text
| Token | Pencil value |
|---|---|
| `--text-primary` | `#0C1D31` |
| `--text-secondary` | `#3D4F63` |
| `--text-muted` | `#7E8FA0` |

### Accent
| Token | Pencil value |
|---|---|
| `--accent` | `#0C1D31` |
| `--accent-hover` | `#162B44` |

### Pills
| Token | Pencil value |
|---|---|
| `--pill-bg` | `#F0F1F3` |

### Context colors
| Token | Pencil value |
|---|---|
| `--ctx-platform` | `#9B6B6B` |
| `--ctx-organization` | `#8B5555` |
| `--ctx-project` | `#4D6356` |
| `--ctx-user` | (not in design; assume Platform palette unless product disagrees) |

Band/chip fills use the context color at low alpha — encode as `color-mix` or pre-mixed hex (`#4D635615`, `#4D635625`/`#4D635630` Project; `#BF959540` Org chip; `#ECD0D080` Platform chip).

### Source badges
| Token | Pencil value |
|---|---|
| `--source-policy` | `#5B7A3D` |
| `--source-manual` | `#7E8FA0` |

### Status
| Token | Pencil value |
|---|---|
| `--status-valid` | `#4D6356` |
| `--status-error` | `#C45B56` |

### Typography
| Token | Pencil value |
|---|---|
| `--font-heading` | `DM Serif Display` |
| `--font-ui` | `DM Sans` |
| `--font-mono` | `JetBrains Mono` |

### Missing / new tokens
| Token | Proposed value | Reason |
|---|---|---|
| **`--node-selected-glow`** | `#A78BFA44` | One-off purple glow used for the selected graph node (Pencil shadow on component `O2HWcY`). Not in the Pencil variable palette today — add it to the token sheet so it's not buried as a magic value. |

---

## 7. Open Questions for the User

Resolve before implementation begins.

1. **Inventory filter strategy on Screen 4 — approve label-stamping via the policy controller?** This commits a small backend change to stamp `knowledge.miloapis.com/relationship-type`, `/subject-kind`, `/source-type` labels on every `ResourceRelationship`. Without it, filters either don't work or scale poorly on the "247 relationships" scenario.
3. **Truncation banner copy fix.** Design hardcodes "500 node limit." Confirm the banner reads the user's submitted `maxNodes` value (and reflects depth-cap truncation we detect client-side until backend gap #3 ships).
4. **App shell reconciliation.** Recommend a single `<AppLayout>` with the top-tab nav always visible and the sidebar collapsing on non-graph routes. Confirm — or do you want two fully separate layouts (top-tab on inventory/catalog, sidebar-only on graph)?
5. **Discovery Policies grid (Screen 5).** Not specified in design. Approve a PolicyCard mirroring TypeCard: name, target RelationshipType, subject Kind, `status.discoveredEdgesCount`, condition status pill.
6. **"Create Type" button on Screen 5.** Hide entirely, or render visible-but-disabled with a tooltip pointing to the GitOps workflow?
7. **"Create Relationship" button on Screen 4.** No design exists. Defer to post-MVP, or do you want a minimal form (subject endpoint, object endpoint, relationshipType, source=Manual) now?
8. **GraphQuery JSON response schema.** Lock the exact `GraphQueryResult` shape (node/edge field names) — needed to finalize `mapGraphQueryResultToCanvasModel`.
9. **`remainingItemCount` support on the aggregated API server.** Confirm it's emitted on paginated LIST responses; if not, the "N of M+" copy needs to fall back to "N+".
10. **User context palette.** The `User` control plane context kind isn't seen in the design. Confirm it shares the Platform palette, or define a dedicated `--ctx-user` color.
