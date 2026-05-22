# Knowledge Graph UI — UX Specification

Derived from `designs/knowledge-graph.pen`. Five screens at 1440×900. Frame IDs in parens.

## Design Tokens

### Color variables

| Token | Value | Purpose |
|---|---|---|
| `--bg-base` | `#FFFFFF` | App background, graph canvas |
| `--bg-card` | `#FFFFFF` | Card / row surface |
| `--bg-surface` | `#F6F6F5` | Subtle inset surface (selects, kind chips) |
| `--bg-sidebar` | `#F6F6F5` | Left sidebar / top nav bar |
| `--bg-elevated` | `#EFEFED` | Modal background, table header, footer |
| `--border` | `#E8E7E4` | 1px dividers, card borders |
| `--border-subtle` | `#EFEFED` | Row separators inside drawer |
| `--accent` | `#0C1D31` | Primary CTA, active tab, focused link, checkbox fill |
| `--accent-hover` | `#162B44` | Hover state for `--accent` surfaces |
| `--text-primary` | `#0C1D31` | Body text, primary labels |
| `--text-secondary` | `#3D4F63` | Secondary labels, inactive nav text |
| `--text-muted` | `#7E8FA0` | Placeholder, helper, breadcrumb, meta |
| `--pill-bg` | `#F0F1F3` | Default pill chip background (Edge Pill, kind chip) |
| `--ctx-platform` | `#9B6B6B` | "Platform" control plane context color |
| `--ctx-organization` | `#8B5555` | "Organization" context color |
| `--ctx-project` | `#4D6356` | "Project" context color |
| `--source-policy` | `#5B7A3D` | "Policy" source badge text |
| `--source-manual` | `#7E8FA0` | "Manual" source badge text |
| `--status-valid` | `#4D6356` | Valid (circle-check) icon |
| `--status-error` | `#C45B56` | Invalid / error (circle-x) icon |

Context bands and pills use the context color at low alpha (e.g. `#4D635615` for the Project band, `#4D635625`/`#4D635630` for Project chip backgrounds, `#BF959540` for Organization chips, `#ECD0D080` for Platform chips). Source badges similarly use `#5B7A3D20` (Policy) and `#7E8FA020` (Manual).

### Typography

| Token | Value | Used for |
|---|---|---|
| `--font-heading` | `DM Serif Display` | Page titles ("Relationship Inventory", drawer "Resource Detail" header) |
| `--font-ui` | `DM Sans` | All UI labels, buttons, body |
| `--font-mono` | `JetBrains Mono` | Resource Kind/Name, namespaces, API versions, edge pill labels, policy names |

Font sizes seen: 9, 10, 11, 12, 13, 14, 15, 16, 18, 20. Weights: normal / 500 / 600 / 700.

### Reusable components

- **`O2HWcY` — component/Graph Node**: pill-shaped frame, 8px corner radius, white fill, 1.5px context-colored stroke, 7×12 padding. Contains:
  - 8×8 ellipse `bw5Er` (indicator dot, filled with context color)
  - mono text `rKvWB` (Kind/Name, 12px)
  - Customizable via descendant overrides: dot fill, text content, stroke color/thickness. Selected variant adds a `#A78BFA44` outer blur shadow (16 blur, 2 spread) and 2px stroke.
- **`elG26` — component/Edge Pill**: 10px corner radius, `--pill-bg`, 3×8 padding, mono 9px muted label (`OjPGp`). Customized via descendant override of the label content (`"Belongs to"`, `"Linked to"`, `"Routes to"`, `"Owns"`, `"AttachedTo"`, `"OwnedBy"`, `"MemberOf"`, `"ScheduledOn"`, etc.).

---

## Screen 1 — Graph Explorer (`Wgb43`)

The primary canvas: a filterable node-link view of the knowledge graph, with the visible graph banded horizontally by control plane context.

### Layout
- **Sidebar** (`M6CqT`, 260px, `--bg-sidebar`, right border 1px):
  - Brand header (`DBZhX`): lucide `share-2` icon (`--accent`, 22px) + "Knowledge Graph" 15px/600.
  - 1px divider.
  - Filter stack (`GknyD`, gap 20):
    - **CONTEXT** group (`sKfWa`): label (11px/600 muted, 0.8 letter-spacing) + select pill (`jqQon`) showing current value ("Organization") with chevron-down. Click → dropdown to pick Platform / Organization / Project (matches `ParentContext` enum).
    - **RELATIONSHIP TYPES** group (`LioI9`): label + 4 checkbox rows (`Gfdv5`, `r7nkQ9`, `ZzqXL`, `Xbx4n`) labeled "Linked to", "Belongs to", "Runs on", "Owned by". All shown checked (16×16 accent square, white check icon). Toggling refilters edges.
    - **DIRECTION** group (`diiyU`): label + segmented control (`X0k5U`) with three pills inside a bordered surface: **Outbound** (selected, accent fill, white text), **Inbound**, **Both** (unselected, secondary text). Choose one.
  - **Run Query** button (`RbPOx`, pinned to bottom via `justifyContent: space_between` on parent): full-width accent pill, white play icon + "Run Query" 13/600. Opens Screen 3 (Query Builder) or re-runs the current query.
- **Main area** (`XTX6m`):
  - **Top bar** (`Xffeh`, 48px, `--bg-surface`, bottom border):
    - Breadcrumb "Knowledge Graph / Explorer" (muted "/" separators, current crumb in primary text).
    - Right-aligned global search (`lcJyA`, 280px, white fill, border, lucide `search` icon + "Search resources..." placeholder).
  - **Graph canvas** (`KoWDV`, fills remaining height, white fill, clipped, free positioning):
    - Two horizontal context **bands** filling canvas width (1180px):
      - Top band: Organization (`#BF959518` fill, 280h)
      - Bottom band: Project (`#4D635615` fill, 350h)
      - 1px dark divider (`#0C1D3140`) between them.
      - Right-edge band labels (`ORGANIZATION`, `PROJECT`), 10/600, ctx-color @ 66% alpha, 1px letter-spacing.
    - 9 graph nodes (refs of `O2HWcY`): User/alice, Org/acme-corp, Team/backend, HTTPProxy/api-proxy (selected, with purple glow + 2px stroke), Workload/auth-svc, Domain/app.example.com, Instance/web-001, Workload/web-app, Instance/worker-001. Each colored by its band.
    - 6 edges drawn as bezier paths (`#1E3A58` 1.5px stroke) with mid-edge edge pills overlaid: "Belongs to" ×2, "Linked to", "Routes to", "Owns" ×2.

### Interactions
- Sidebar filter changes refetch / refilter the canvas immediately.
- Clicking the **Run Query** button opens Screen 3 (Query Builder modal) over the canvas.
- Top-bar **search** opens a resource picker; selecting a result re-centers/expands the graph from that node.
- Clicking a **graph node** → opens Screen 2 (Node Detail Drawer) and highlights the selected node (purple glow + 2px stroke).
- Clicking an **edge pill** → drill into the underlying `ResourceRelationship` (could route to Screen 4 filtered, or show inline detail).
- Hovering a node shows a tooltip with full kind/name + context (inferred).
- Node colors and band placement key off the resource's `ControlPlaneContextRef.Kind`.

### States needed
- Loading: skeleton canvas / spinner.
- Empty: "No relationships in this view" with hint to broaden filters.
- Error: inline banner with retry.
- Truncated (>maxNodes): show warning banner like Screen 3's "Results truncated — 500 node limit reached".
- Selected node: purple glow (`#A78BFA44`, blur 16, spread 2) + 2px stroke (matches `PHIqc`).

---

## Screen 2 — Node Detail Drawer (`ZWuUD`)

Base layer = Screen 1 underneath; a semi-transparent overlay (`GoWbL`, `#0C1D3140`, 1180×852 starting at x=260, y=48) dims the canvas. A right-side drawer slides in.

### Drawer (`v8ADA`, 380×900, `--bg-surface`, left border, large left-cast shadow)
- **Header** (`zm3pK`, 16×20 pad, space-between): "Resource Detail" 14/600 heading-font + lucide `x` close icon.
- 1px divider.
- **Identity block** (`whaOv`, 16×20 pad, gap 12, vertical):
  - Row: pill chip `HTTPProxy` (mono 11, pill-bg) + name `api-proxy` (mono 16/600).
  - Row "Namespace" / `ingress-system` (mono 12).
  - Row "Context" / pill `Project` (in ctx-project color on `#4D635630` background).
  - Row "API Version" / `networking.k8s.io/v1` (mono 12).
- 1px divider.
- **Tabs** (`DCSQH`, 10×16 pad inside 0×20): **Outbound** (active — primary text + 2px accent bottom border) and **Inbound** (muted).
- 1px divider.
- **Relationship list** (`MxJaF`, vertical, fills remaining height). Each row (`BQ09Z` / `s5ZJS` / `eOoI5` / `N76UUj`) is 12×20 pad with bottom border-subtle:
  - Top line: small `--pill-bg` chip showing the relationship type label ("Linked to" or similar, mono 10 muted) + mono 12 target Kind/Name.
  - Bottom line: source badge ("Policy" green or "Manual" gray, small chip) + right-aligned "Traverse →" link (accent text).
  - Examples in design: Domain/app.example.com (Linked to / Policy), Workload/auth-svc (Linked to / Manual), HTTPProxy/api-routes (Linked to / Policy), Network/prod-network (Linked to / Manual).

### Data fields
Resource Kind, Name, Namespace, ControlPlaneContextRef.Kind, API Version, and the resource's outbound/inbound relationships (type label, peer Kind/Name, source = Policy|Manual).

### Interactions
- Close (`aOnan`) → dismiss drawer, restore canvas.
- Click backdrop → same.
- Tab toggle Outbound ↔ Inbound switches the listed direction.
- "Traverse →" on a row → re-centers Screen 1 on that peer (selects it as the new root) or opens a new detail drawer.
- Clicking the row's Kind/Name target → navigates to that resource's detail.

### States
- Loading: skeleton list.
- Empty (no outbound/inbound edges for selected tab): "No outbound relationships".
- Error fetching peer details: row shows error icon inline.

---

## Screen 3 — Graph Query Builder (`d15pmD`)

Base = Screen 1 dimmed by full-canvas overlay `MGeQ2` (`#0C1D3160`); a centered modal floats above with an inline warning at top.

### Modal (`zKgzA`, 520×~580, `--bg-elevated`, 12px radius, large shadow)
- **Banner** (`wLaMo`, full width): `triangle-alert` icon `#B8960A` + "Results truncated — 500 node limit reached" `#8B6B00`, on `#5B7A3D15`. (Shown when the previous query hit the cap.)
- **Header** (`XZKcd`): "Graph Query Builder" 18/600 heading-font + lucide `x` close.
- **Body** (`KbP2c`, gap 16):
  - **Root Resource** select (`MV1l8`, full-width, 8×12 pad, surface fill, 1px border) showing current selection `HTTPProxy/api-proxy` (mono) + small `Project` context pill + chevron-down. Click → resource picker.
  - **Relationship Types** (`u6We6`, two-column grid, gap 16): four checkboxes — "Linked to" ✓, "Belongs to" ✓, "Runs on" ☐, "Owned by" ✓. Each is a 16×16 square (filled accent for ✓, border-only for ☐) + label.
  - **Direction** (`dgY4d`, horizontal radio group): "Outbound" (filled accent dot inside a circle), "Inbound" (empty ring), "Both" (empty ring). Single-select.
  - **Max Depth** (`nsrLW`): label + current value mono "5" on the right; slider track (border) with accent fill up to ~50%, accent ellipse handle. Min "1", max "10" labels.
  - **Max Nodes** (`HGZQk`): same pattern — value "500", min "50", max "1000".
- 1px divider.
- **Footer** (`xZMJe`, right-aligned): "Run Query" accent button (play icon + label, 10×24 pad).

### Spec-to-API mapping
This dialog composes a `GraphQuery` POST body:
- Root resource → `spec.root` (Kind+Name+contextRef from picker).
- Relationship Types checkboxes → `spec.traverse.relationshipTypes[]`.
- Direction → `spec.traverse.direction` (`Outbound|Inbound|Both`).
- Max Depth → `spec.traverse.maxDepth` (1–10).
- Max Nodes → `spec.traverse.maxNodes` (50–1000, default 500).

### Interactions
- Click X / click overlay → close, no changes.
- Run Query → POST `GraphQuery`, close modal, render result graph in Screen 1.
- All form controls are immediate-update (no separate Apply step).

### States
- Loading the picker / running the query: show spinner inside the Run button.
- Validation error (e.g. depth > 10): inline red helper text under the slider.
- Truncation banner is conditional on previous run.
- Error state: replace banner with red error message + retry.

---

## Screen 4 — Relationship Inventory (`A9v4Q`)

Different top-level chrome from Screens 1–3: this and Screen 5 share a single horizontal app nav bar instead of a sidebar.

### Layout
- **Top nav bar** (`AVR6T`, 56h, `--bg-sidebar`, 0×24 pad, space-between):
  - Left: lucide `hexagon` accent + "Knowledge Graph" 15/600 + three tabs ("Graph Explorer" muted, "Relationships" active = primary text 13/600 + 2px accent underline (48w), "Types & Policies" muted).
  - Right: lucide `search` + `bell` icons (secondary), 28px accent avatar ellipse.
- 1px divider.
- **Page header** (`bMGPp`, in a 24×32-padded content area):
  - Title "Relationship Inventory" 20/700 heading + subline "Browse and manage all resource relationships across control plane contexts" (secondary 13).
  - Right cluster: **Export** button (card surface, border, download icon + "Export") and **Create Relationship** primary button (accent, plus icon + label).
- **Filter bar** (`ybtLn`, gap 10):
  - Search input (`ooMYy`, 240w, white, border, "Search relationships..." placeholder).
  - Four pill-style dropdown filters with lucide icons: "Context: All" (`layers`), "Type: All" (`git-branch`), "Source: All" (`zap`), "Validity: All" (`circle-check`). Each has a chevron-down.
  - Spacer pushes the count right.
  - Right: "247 relationships" muted count.
- **Table** (`A90E7t`, card, rounded 8, 1px border):
  - **Header row** (`cRt2C`, 44h, `--bg-elevated`, 0×16 pad): columns sized — Relationship 180, From 220, To 220, Source 160, Valid 70, Created 130. Labels are 11/600 muted, 0.5 letter-spacing.
  - **Body rows** (48h, 0×16 pad). Striped via row fill (some `--bg-surface`, some `--bg-card`). One row (`dZfhN`) is **selected/highlighted**: fill `#EDF2F7`, 1px accent stroke.
    - **Relationship** col: rounded-12 pill `--pill-bg` with the type label in accent color 11/500 (e.g. "Linked to", "Belongs to", "Instance of", "Owned by", "Depends on", "Routes to").
    - **From** col: small context chip ("Plat" / "Orga" / "Proj" — 9/600 in ctx-color on tinted ctx-fill) + mono 11 Kind/Name (e.g. `Domain/app.example.com`, `User/alice@acme.io`, `Workload/order-svc`).
    - **To** col: same chip + mono Kind/Name format.
    - **Source** col: source badge ("Policy" green / "Manual" gray) optionally followed by mono 10 policy name (e.g. `domain-attach`, `instance-placement`, `ownership`, `domain-link`, `sa-membership`, `route-discovery`). Manual rows show only the badge.
    - **Valid** col: centered icon — `circle-check` `--status-valid` or `circle-x` `--status-error`.
    - **Created** col: mono 11 timestamp `YYYY-MM-DD HH:mm`.
  - **Footer** (`K5c7JK`, 48h, `--bg-elevated`, top border): left "Showing 1–8 of 247 relationships" muted; right pagination — `←` arrow, page numbers (1 selected = accent fill / white text, others transparent / secondary), `…` ellipsis, last page "31", `→` arrow. 32×32 button cells, 6 radius.

### Data per row
`ResourceRelationship` fields: `spec.type` (display name), `from{contextKind, kind, name}`, `to{contextKind, kind, name}`, `metadata.source` ("Policy" + policy name, or "Manual"), `status.valid` (bool), `metadata.creationTimestamp`.

### Interactions
- Row click → opens Screen 2-style detail drawer or navigates to a relationship detail route.
- Sort by clicking column headers (implied by header-row styling; no sort icon shown but expected).
- Export → downloads filtered set (CSV/JSON).
- Create Relationship → opens a creation modal (not in design — to be defined).
- Each filter pill opens a dropdown menu. Search debounces and refilters.
- Pagination → navigate pages.

### States
- Loading: row skeletons.
- Empty (no rows match filters): empty-state with "Clear filters" link.
- Selected row highlight as shown.
- Invalid relationship row: still rendered, with the red `circle-x` icon. Could show validation reason on hover/click.

---

## Screen 5 — Types & Policies Catalog (`EQweO`)

Same top nav bar as Screen 4 (the "Types & Policies" tab is now active with underline). Subnav switches between **Relationship Types** and **Discovery Policies**.

### Layout
- **Top nav bar** (`nEu3A`): identical to Screen 4 but with the third tab active.
- **Page header** (`UaQ0f`): "Types & Policies Catalog" 20/700 + subline "Manage relationship type schemas and auto-discovery policies". Right: **Create Type** primary accent button.
- **Subnav tabs** (`YowPq`, bottom-bordered):
  - **Relationship Types** (active): icon `boxes` + label 13/600 + count badge "8" (rounded-10 chip with accent fill at 0.2 opacity, accent text). 2px accent bottom border.
  - **Discovery Policies** (inactive): icon `scroll` + muted label + count badge "12" on pill-bg with muted text.
- **Type card grid** (`tGtpV`): two rows of three cards (`Hy8Ko`, `NodmI`). Each card (`FIBZC` etc.) is `--bg-card`, 10 radius, 1px border, 20 pad, gap 14, vertical.
  - **Header row** (`hjc3N`, space-between):
    - Type name in 16/700 UI font (e.g. "Linked to", "Belongs to", "Instance of", "Owned by", "Depends on", "Routes to").
    - Edge-count: lucide `git-commit-horizontal` icon + "42 edges" (or similar — text from `XgoZj`) in muted 11.
  - **Trait badges** (`Q8cKw`, gap 6): two small pill-bg chips, 10/600 secondary — cardinality ("Many to One") and directedness ("Directed").
  - **Schema row** (`MoXic`, gap 8, horizontal):
    - Left "from kinds" box (`mSj5b`, fills, surface fill, 6 radius, 8×10 pad): one or more small kind chips (e.g. "Domain", "HTTPProxy") in mono 10 colored by context.
    - Middle: muted `arrow-right` lucide icon.
    - Right "to kinds" box (`Z2Vax`): same chip pattern (e.g. "Network").

### Data per card
`RelationshipType` schema: `metadata.name` (display label), counts (number of `ResourceRelationship` instances of this type, fetched separately), `spec.cardinality`, `spec.directed`, `spec.from.kinds[]`, `spec.to.kinds[]`. Kind chips are colored by which `ControlPlaneContext` they're typically declared in (organization color in the sample).

### Interactions
- Card click → opens the RelationshipType detail (schema, sample edges, policies that produce it).
- "Create Type" button → opens type-creation form (not in design).
- Switching to **Discovery Policies** tab swaps the grid for a list/grid of `RelationshipPolicy` resources (not designed — to be specced separately).
- Edge-count clickable → routes to Screen 4 pre-filtered by `Type: <thisType>`.

### States
- Loading: card skeletons.
- Empty: "No relationship types defined yet. Create one to start."
- A type with zero instances: edge-count shows "0 edges" muted; card still rendered.
- Long kind lists: chips wrap; truncate with "+N" if very long.

---

## Cross-screen patterns

- **Context coloring** is the visual backbone. Every node, badge, and band keys off the resource's `ControlPlaneContextRef.Kind` (`Platform`, `Organization`, `Project`, with `User` likely sharing the platform palette — not seen in design).
- **Source coloring**: green = Policy-discovered, gray = Manual. Always rendered as a chip.
- **Mono font** is reserved for machine-identifier strings (Kind/Name, namespaces, API versions, policy names, timestamps).
- **Top chrome** differs by mode: Screens 1–3 use a left sidebar (graph-centric workflow); Screens 4–5 use a top tab nav (table/catalog workflow). The two app nav bars are not stitched together in the design — implementation must reconcile this (the Graph Explorer / Relationships / Types & Policies tabs on Screens 4–5 link to the Screen 1 sidebar mode).
- **Primary CTA style** is consistent: accent fill, white text/icon, 6–8 radius.

## Notable findings

1. **Two distinct shells.** Screens 1–3 are sidebar + canvas; Screens 4–5 are top-tab + content. A frontend implementer must decide whether to nest these under one app shell with conditional rendering or treat the graph view as a "focused mode" route.
2. **Filter sidebar duplicates the Query Builder modal.** Screen 1's sidebar controls (Context, Relationship Types, Direction) are a subset of Screen 3's modal (which adds Root Resource, Max Depth, Max Nodes). Likely the sidebar is for live re-filtering of an already-loaded result, while the modal defines a fresh query.
3. **Truncation banner** is a first-class UX concern (matches the backend's `maxNodes` limit) and should be implemented as a reusable banner pattern.
4. **No "Discovery Policies" content** is designed on Screen 5 — the tab exists with a count of 12 but the panel itself is unspecified. This is a gap for the implementation plan.
5. **No creation flows** are designed: Create Relationship (Screen 4) and Create Type (Screen 5) buttons exist but their forms are not in the .pen file.
6. **The "Routes to" sample edge** on Screen 1 (HTTPProxy → Workload) crosses the context band divider — the design supports cross-context edges visually with a node-to-node bezier through the band line.
7. **Selected-node treatment** uses a purple glow (`#A78BFA44`) that doesn't appear in the token palette — it's a one-off effect color. Worth adding as a token (e.g. `--node-selected-glow`).
8. **User avatar** in the Screen 4/5 top bar is a plain accent-filled ellipse — no image, no initials. Implementation should add initials/photo.
9. **Source badge** appears in two sizes (Screens 2 and 4 differ in chip dimensions); standardize.
10. **No mobile or responsive treatment** is in the design. Assume desktop-only for v1.
