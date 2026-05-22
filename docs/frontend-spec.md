# Knowledge Graph UI — Frontend Architecture Spec

This document specifies the React frontend architecture for the Milo OS Knowledge Graph UI. It speaks to the aggregated Kubernetes API server exposing `knowledge.miloapis.com/v1alpha1` and renders the five screens defined in the Pencil design.

---

## 1. Framework & Tooling

| Choice | Selection | Rationale |
|---|---|---|
| Language | **TypeScript** (strict mode) | All k8s payloads are typed; CRD schemas generate cleanly to TS types. |
| UI runtime | **React 18** | Concurrent features (`useTransition`, Suspense) are useful for the graph canvas and large lists. |
| Build tool | **Vite 5** | Fast HMR, first-class TS + JSX, ESM-native. Matches the rest of the Milo OS tooling direction. |
| Package manager | **pnpm** | Strict, fast, reproducible. Workspaces leave room for a `packages/k8s-client` split later. |
| Lint / format | **ESLint** (typescript-eslint, react-hooks) + **Prettier** | Standard. |
| Testing | **Vitest** (unit) + **Playwright** (e2e against a kind cluster) | Vitest shares Vite config; Playwright matches the existing chainsaw e2e mindset. |
| Node | **20 LTS** | |

### Hard dependencies (production)

- `react`, `react-dom`
- `react-router-dom` v6
- `@tanstack/react-query` v5 — server state, watch fan-in, mutations
- `react-force-graph-2d` — graph canvas (see §6)
- `zustand` — small UI-only global store (selection, drawer open/close, current context)
- `react-hook-form` + `zod` — Query Builder + Policy create forms with schema-level validation
- `@radix-ui/react-*` primitives (Dialog, Tabs, Popover, Tooltip, DropdownMenu) — unstyled accessible building blocks; we style with our own tokens
- `clsx` — class composition
- `date-fns` — relative "Age" formatting

No CSS framework. Styling is hand-rolled CSS Modules + design tokens (§7). No Tailwind — token vocabulary is too project-specific and we want exact parity with the Pencil variables.

---

## 2. Routing

`react-router-dom` v6 with a single root layout and nested routes per screen. The Graph Query Builder and Node Detail Drawer are **overlays** on top of routes, not standalone routes — they're driven by URL search params so they're shareable.

```
/                                          → redirect to /graph
/graph                                     → Screen 1: Graph Explorer
  ?node=<kind>/<ns>/<name>                 → opens Screen 2: Node Detail Drawer
  ?query=open                              → opens Screen 3: Query Builder modal
/relationships                             → Screen 4: Relationship Inventory
  ?type=<rt>&kind=<k>&source=<s>&page=<n>  → filter + pagination
/catalog                                   → Screen 5: Types & Policies Catalog
  #types | #policies                       → anchor to section
/catalog/policies/new                      → Create RelationshipPolicy form (modal route)
```

Why search params instead of nested routes for the drawer/modal: keeps the graph mount stable when opening a node, avoids canvas remount cost, and gives deep-linkable selection out of the box.

---

## 3. Component Hierarchy

### Top-level shell

```
<App>
 └ <QueryClientProvider>
   └ <AuthProvider>           // injects identity + bearer token into ApiClient
     └ <BrowserRouter>
       └ <AppLayout>
         ├ <Sidebar/>          // nav: Graph / Relationships / Catalog
         ├ <TopBar/>           // breadcrumbs, context picker, user menu
         └ <Outlet/>           // route content
```

### Screen 1 — Graph Explorer (`/graph`)

```
<GraphExplorerPage>
 ├ <GraphToolbar/>             // search, depth selector, "Open Query Builder" button, layout toggle
 ├ <GraphCanvas>               // react-force-graph-2d, fills available space
 │  ├ <ContextBands/>          // SVG overlay (Org/Project layer rectangles)
 │  ├ <GraphNode/> (per node)  // custom rendered: indicator dot + Kind/Name pill
 │  └ <GraphEdge/> (per link)  // labeled edge pill in mid-link
 ├ <GraphLegend/>              // context color key
 ├ <NodeDetailDrawer/>         // mounted when ?node=
 └ <QueryBuilderModal/>        // mounted when ?query=open
```

### Screen 2 — Node Detail Drawer (overlay)

```
<NodeDetailDrawer>             // Radix Dialog in side-drawer mode
 ├ <DrawerHeader/>             // Kind badge, Name, context chip, close
 ├ <Tabs>
 │  ├ <ConnectionsTab>
 │  │  ├ <RelationshipList direction="inbound"/>
 │  │  └ <RelationshipList direction="outbound"/>
 │  └ <MetadataTab>
 │     ├ <IdentityFields/>     // APIGroup/Kind/Name/Namespace/UID
 │     └ <ControlPlaneContextChip/>
 └ <DrawerFooter/>             // "Center in graph", "Open in K8s console"
```

### Screen 3 — Graph Query Builder (overlay modal)

```
<QueryBuilderModal>            // Radix Dialog
 └ <QueryBuilderForm>          // react-hook-form + zod
    ├ <RootResourceFieldset>   // APIGroup, Kind, Name, Namespace, ControlPlaneContextRef.{Kind,Name}
    ├ <TraversalFieldset>      // MaxDepth slider (1-20), MaxNodes slider (1-1000), Direction radios
    ├ <RelationshipTypesFilter/> // multi-select populated from RelationshipType list
    ├ <FormErrors/>
    └ <FormActions/>           // Cancel, Run Query
```

On submit: POST to `…/graphqueries`, on success replace canvas data and close modal.

### Screen 4 — Relationship Inventory (`/relationships`)

```
<RelationshipInventoryPage>
 ├ <InventoryHeader/>          // "247 relationships", export?
 ├ <InventoryFilters/>         // RelationshipType select, Subject Kind select, Source select
 ├ <RelationshipTable>
 │  ├ <Columns: RelationshipType | Subject | Object | Source | Age | Actions>
 │  └ <Row/> per ResourceRelationship
 └ <Pagination/>                // cursor-based (k8s continue token)
```

### Screen 5 — Types & Policies Catalog (`/catalog`)

```
<CatalogPage>
 ├ <CatalogTabs/>              // RelationshipTypes | RelationshipPolicies
 ├ <RelationshipTypesGrid>
 │  └ <TypeCard/>              // read-only; click → details popover
 └ <PoliciesGrid>
    ├ <CreatePolicyButton/>    // routes to /catalog/policies/new
    └ <PolicyCard/>            // delete action menu
<PolicyCreateModal>            // mounted at /catalog/policies/new
```

### Reusable primitives (`src/components/`)

- `<ResourceBadge kind name contextColor>` — the Kind/Name pill (matches Pencil component `O2HWcY`)
- `<EdgePill label>` — relationship type chip (Pencil component `elG26`)
- `<ContextChip kind name>` — Platform/Org/Project/User color-coded chip
- `<EmptyState title description action>`
- `<ErrorBoundary fallback>`
- `<DataTable columns rows pagination>` — generic table for the inventory and any future lists
- `<KindIcon kind>` — small glyph by Kubernetes Kind
- `<RelativeTime ts>` — wraps date-fns `formatDistanceToNow`

---

## 4. State Management

Three layers, each owns a distinct concern. Do not mix.

### 4.1 Server state — TanStack Query

Every k8s read is a `useQuery`. Every write is a `useMutation`. Query keys mirror the k8s URL shape so cache invalidation is mechanical:

```ts
queryKey: ['knowledge', 'v1alpha1', 'relationshippolicies', { namespace }]
queryKey: ['knowledge', 'v1alpha1', 'resourcerelationships', { namespace, listOpts }]
queryKey: ['knowledge', 'v1alpha1', 'relationshiptypes']
```

`GraphQuery` POSTs are `useMutation` — the response payload becomes the canvas's data source. The result is cached under `['graphquery', <hash of spec>]` so navigating away and back replays the last query without re-POSTing.

Defaults:
- `staleTime: 30_000` for list queries
- `refetchOnWindowFocus: true`
- `retry: (failures, err) => err.code !== 401 && err.code !== 403 && failures < 2`

### 4.2 URL state — react-router search params

- Selected node (`?node=`), open drawer state, open modal state, inventory filters, pagination cursor.
- Deep-linkable, refresh-safe, no extra store needed.

A small `useSearchParamState<T>(key, codec)` hook wraps `useSearchParams` for typed reads/writes.

### 4.3 UI-only global state — Zustand

One store, four slices:

```ts
useUIStore = create<{
  // Current control plane context (Platform / Org / Project / User + name)
  currentContext: ControlPlaneContextRef;
  setCurrentContext: (c: ControlPlaneContextRef) => void;

  // Graph canvas controls
  layout: 'force' | 'hierarchical';
  showContextBands: boolean;
  // ... toggles only — no data

  // Last submitted GraphQuery spec (so the modal can pre-fill)
  lastQuerySpec?: GraphQuerySpec;

  // Toast / notification queue
  toasts: Toast[];
}>()
```

Auth/identity lives in a separate `AuthContext` (React context, not Zustand) because it's a one-shot at boot.

### 4.4 What does NOT belong in global state

- Server data (use React Query).
- Form values (use react-hook-form).
- Selected node, drawer open/closed (use URL).
- Modal open/closed (use URL).

---

## 5. API Client Layer

### Authentication

Two supported modes, picked at runtime via build-time env var `VITE_AUTH_MODE`:

1. **`oidc`** (default for hosted) — UI redirects through the Milo OS OIDC provider, stores the bearer token in memory + `sessionStorage`, attaches `Authorization: Bearer <token>` to every request. Silent refresh via the existing Milo OIDC flow used elsewhere.
2. **`proxy`** (default for local dev) — UI is served behind `kubectl proxy` or an authenticating proxy that injects credentials. The UI sends no `Authorization` header; cookies handle auth. Detected by env var.

No raw kubeconfigs in the browser. Never. The token never leaves the running tab.

### Client structure

```
src/api/
├── client.ts            // low-level fetch wrapper (auth, retries, k8s error decoding)
├── errors.ts            // K8sStatusError class wrapping {code, reason, message, details}
├── types/               // generated TS types from CRD OpenAPI (script: pnpm gen:types)
│   ├── RelationshipType.ts
│   ├── RelationshipPolicy.ts
│   ├── ResourceRelationship.ts
│   ├── GraphQuery.ts
│   └── meta.ts          // ObjectMeta, ListMeta, ControlPlaneContextRef
└── resources/
    ├── relationshipTypes.ts    // list, get
    ├── relationshipPolicies.ts // list, get, create, delete
    ├── resourceRelationships.ts // list (with field selectors), get, create, delete
    └── graphQueries.ts          // submit(spec) → GraphQueryResult
```

Every resource module exports plain async functions and matching React Query hooks (`useRelationshipPolicies(ns)`, `useCreateRelationshipPolicy()`, …). Hooks are thin — the work happens in the function so it's also callable from non-React contexts (Playwright fixtures, scripts).

### Field selectors and pagination

- List queries accept `{ labelSelector, fieldSelector, limit, continue }` and pass through to the k8s API.
- Inventory pagination uses k8s `limit` + `continue` tokens (see §8).

### Code generation

A one-shot script (`scripts/gen-types.ts`) downloads the aggregated API OpenAPI document from a live cluster (`kubectl get --raw /openapi/v3/apis/knowledge.miloapis.com/v1alpha1`) and emits TS types via `openapi-typescript`. Checked-in output, regenerated when CRDs change.

---

## 6. Graph Visualization

### Choice: **`react-force-graph-2d`**

| | react-force-graph-2d | raw d3-force + custom canvas | vis-network |
|---|---|---|---|
| Out-of-box force layout | ✅ | ❌ (you wire it) | ✅ |
| Canvas (not SVG) rendering | ✅ | depends | ✅ |
| Custom node rendering (Kind/Name pill) | ✅ via `nodeCanvasObject` | ✅ | ⚠️ harder |
| TypeScript | ✅ types ship | ✅ for d3 | ⚠️ partial |
| 100–1000 nodes performance | ✅ smooth (canvas + WebGL variant available) | ✅ if you build it | ⚠️ slows past ~500 |
| React integration | ✅ first-class | ❌ glue code | ⚠️ imperative API |
| Edge labels | ✅ `linkCanvasObject` | manual | ✅ |
| Bundle size | ~150 KB gz | ~80 KB gz (d3 subset) | ~250 KB gz |

`react-force-graph-2d` wins on time-to-implement and matches the design's custom node/edge styling needs. If perf degrades past ~1500 nodes we can swap to the `-webgl` variant from the same package without rewriting our adapter.

### Adapter

`<GraphCanvas>` accepts a normalized `{ nodes, links }` model. We do **not** pass k8s objects directly:

```ts
type GraphNode = {
  id: string;                  // `${kind}/${namespace}/${name}`
  kind: string;
  name: string;
  namespace?: string;
  contextKind: 'Platform' | 'Organization' | 'Project' | 'User';
  contextName: string;
};
type GraphLink = {
  source: string;
  target: string;
  relationshipType: string;
  source_origin: 'Manual' | 'Policy';
};
```

A pure mapper (`mapGraphQueryResultToCanvasModel`) converts the API response. Easy to unit test, easy to mock.

### Context bands

The design overlays Org/Project "bands" behind the nodes. Implemented as an absolutely positioned SVG layer that reads node positions from `react-force-graph-2d`'s `onEngineTick` callback and draws convex hulls (via `d3-polygon.polygonHull`) around nodes sharing a context. Toggled by `useUIStore.showContextBands`.

---

## 7. Design Tokens

CSS custom properties on `:root`, defined in `src/styles/tokens.css`. One source of truth, imported once in `main.tsx`.

```css
:root {
  /* Backgrounds */
  --bg-base: #0b0d10;
  --bg-surface: #11141a;
  --bg-elevated: #171b22;
  --bg-card: #1b202a;
  --bg-sidebar: #0e1116;

  /* Text */
  --text-primary: #e7ecf3;
  --text-secondary: #aab3c0;
  --text-muted: #6b7484;

  /* Accent */
  --accent: #6aa6ff;

  /* Borders */
  --border: #232a36;
  --border-subtle: #1a212c;

  /* Typography */
  --font-ui: 'Inter', system-ui, sans-serif;
  --font-heading: 'Inter', system-ui, sans-serif;
  --font-mono: 'JetBrains Mono', ui-monospace, monospace;

  /* Context colors */
  --ctx-platform: #c084fc;
  --ctx-organization: #60a5fa;
  --ctx-project: #34d399;
  --ctx-user: #f59e0b;

  /* Pills */
  --pill-bg: #232a36;
}
```

Concrete values are **placeholders** — exact hex codes come from the Pencil `get_variables` output (UX agent populates). The variable names are fixed; only the values are filled in from the design.

Consumption: CSS Modules per component use `var(--foo)` directly. No JS access to tokens (avoids drift). A single `theme.dark` body class is reserved for future light-mode support — current design is dark-only.

---

## 8. Key Technical Decisions

### Pagination — k8s continue tokens

- `LIST` requests pass `limit=50`. Server returns `metadata.continue` if more pages exist.
- The "247 relationships" total is **not** available from a single list call (k8s doesn't return total counts). Two options:
  1. Show "247+ relationships" only when the server hasn't returned a `remainingItemCount` annotation.
  2. Use `metadata.remainingItemCount` (k8s 1.27+) — added to first response when `limit` is set; `total = items.length + remainingItemCount`. Adopt this; document the lower bound for older clusters.
- Pagination state lives in the URL (`?page=<continueToken>` round-tripped).

### Live updates — watch over polling

- Inventory + Catalog: use k8s `?watch=true&resourceVersion=…` over a streaming `fetch` (ReadableStream). One `useWatch(resource, opts)` hook normalizes ADDED/MODIFIED/DELETED events into React Query cache updates via `queryClient.setQueryData`.
- Reconnect strategy: exponential backoff with jitter, capped at 30s. On 410 Gone, restart from a fresh list (the canonical k8s pattern).
- Graph Explorer canvas: **no live watch.** A GraphQuery is a point-in-time snapshot by design. A "Refresh" button re-submits the last spec. Auto-refresh is opt-in (off by default).

### Error boundaries

- One `<RouteErrorBoundary>` per route renders a recovery panel with the k8s status reason ("Forbidden", "NotFound", etc.).
- One `<GraphErrorBoundary>` around `<GraphCanvas>` so a render crash from malformed query output doesn't take down the toolbar.
- 401 responses: trigger token refresh once; on second 401, redirect to login.
- 403 responses: render a permission-denied empty state with the missing verb + resource, not a generic error.

### Authn/Authz UX

- The UI does not pre-check permissions. It surfaces 403s from the server inline (per-row "Cannot delete" tooltip, greyed create button) using a small `useCanI(verb, resource, ns)` hook that issues `SelfSubjectAccessReview` POSTs. Cached for 60s.

### Accessibility

- All overlays via Radix (focus trap, ARIA, ESC-to-close handled).
- Graph canvas has a parallel keyboard-navigable list view toggle (Tab cycles nodes, Enter opens drawer) — designed in §3 as a future enhancement, scaffolded now.
- Color is never the sole signal: context chips have the context Kind name as text, not just color.

### Performance budgets

- Initial JS bundle (gz): < 300 KB excluding `react-force-graph-2d`.
- Time to interactive on `/graph` empty state: < 1.5 s on M1.
- 500-node canvas: > 30 fps during drag.

### What we deliberately are NOT doing

- No service worker / offline mode. The k8s API does not lend itself to offline mutations.
- No SSR. The app is fully client-rendered; the k8s API server is the only backend.
- No Redux. Three-layer state model (§4) covers every case.
- No GraphQL gateway. All access is direct to the aggregated API.

---

## 9. File / Folder Structure

```
ui/                              # new directory at repo root
├── package.json
├── pnpm-lock.yaml
├── tsconfig.json
├── vite.config.ts
├── index.html
├── playwright.config.ts
├── public/
│   └── favicon.svg
├── scripts/
│   └── gen-types.ts             # openapi-typescript generator
└── src/
    ├── main.tsx
    ├── App.tsx
    ├── router.tsx
    ├── auth/
    │   ├── AuthProvider.tsx
    │   ├── oidc.ts
    │   └── token.ts
    ├── api/
    │   ├── client.ts
    │   ├── errors.ts
    │   ├── watch.ts
    │   ├── types/               # generated, gitignored except index.ts
    │   └── resources/
    │       ├── relationshipTypes.ts
    │       ├── relationshipPolicies.ts
    │       ├── resourceRelationships.ts
    │       └── graphQueries.ts
    ├── components/              # reusable primitives (§3)
    │   ├── ResourceBadge/
    │   ├── EdgePill/
    │   ├── ContextChip/
    │   ├── DataTable/
    │   ├── EmptyState/
    │   ├── ErrorBoundary/
    │   ├── KindIcon/
    │   └── RelativeTime/
    ├── layout/
    │   ├── AppLayout.tsx
    │   ├── Sidebar.tsx
    │   └── TopBar.tsx
    ├── screens/
    │   ├── graph/
    │   │   ├── GraphExplorerPage.tsx
    │   │   ├── GraphCanvas.tsx
    │   │   ├── ContextBands.tsx
    │   │   ├── GraphToolbar.tsx
    │   │   ├── GraphLegend.tsx
    │   │   └── mapGraphQueryResult.ts
    │   ├── nodeDetail/
    │   │   ├── NodeDetailDrawer.tsx
    │   │   ├── ConnectionsTab.tsx
    │   │   ├── MetadataTab.tsx
    │   │   └── RelationshipList.tsx
    │   ├── queryBuilder/
    │   │   ├── QueryBuilderModal.tsx
    │   │   ├── QueryBuilderForm.tsx
    │   │   ├── schema.ts        # zod
    │   │   └── fieldsets/
    │   ├── inventory/
    │   │   ├── RelationshipInventoryPage.tsx
    │   │   ├── InventoryFilters.tsx
    │   │   └── RelationshipTable.tsx
    │   └── catalog/
    │       ├── CatalogPage.tsx
    │       ├── RelationshipTypesGrid.tsx
    │       ├── PoliciesGrid.tsx
    │       ├── TypeCard.tsx
    │       ├── PolicyCard.tsx
    │       └── PolicyCreateModal.tsx
    ├── state/
    │   └── useUIStore.ts        # zustand
    ├── hooks/
    │   ├── useSearchParamState.ts
    │   ├── useWatch.ts
    │   └── useCanI.ts
    ├── styles/
    │   ├── tokens.css           # design tokens (§7)
    │   ├── reset.css
    │   └── global.css
    └── test/
        ├── setup.ts
        └── mocks/
            └── apiHandlers.ts   # msw handlers for unit tests

test/
└── e2e-ui/                      # Playwright tests against a kind cluster
```

---

## 10. Open Questions for Review

These items need a decision before implementation starts. Flagged for the team lead and the review task (#4):

1. **OIDC integration** — does Milo OS already expose a public OIDC discovery URL for browser apps, or do we need a new client registration?
2. **GraphQuery response shape** — the spec says result is in the response body; we need the exact JSON schema (nodes/edges format) to finalize `mapGraphQueryResultToCanvasModel`. (Backend agent, task #3.)
3. **`remainingItemCount` support** — confirm the aggregated API server emits it. If not, "N relationships" copy will need to change.
4. **CORS** — the k8s API server typically does not enable CORS. We will need either a same-origin reverse proxy in the deployment or explicit CORS config on the aggregated server.
5. **Watch over aggregated API** — confirm the aggregated `GraphQuery` apiserver supports streaming watch for `resourcerelationships` (it should, since it implements `storage.Interface`).
