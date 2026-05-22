import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import * as Checkbox from "@radix-ui/react-checkbox";
import { apiClient, type KubernetesList } from "../../api/client";
import type { RelationshipType } from "../../api/resources/relationshipType";
import type { ControlPlaneContextKind, ControlPlaneContextRef, ResourceRelationship } from "../../api/resources/resourceRelationship";
import { useAppStore } from "../../state/store";
import type { GraphQuerySpec, TraversalDirection } from "../../api/resources/graphQuery";
import { type QueryHistoryEntry } from "./queryHistory";
import styles from "./GraphSidebar.module.css";

// ── shared data hook ────────────────────────────────────────────────────────

function useRelationshipsData() {
  return useQuery({
    queryKey: ["known-resource-types"],
    queryFn: () =>
      apiClient.get<KubernetesList<ResourceRelationship>>(
        "/apis/knowledge.miloapis.com/v1alpha1/resourcerelationships?limit=500",
      ),
    staleTime: 60_000,
  });
}

// ── derived types ───────────────────────────────────────────────────────────

interface ResourceType { kind: string; apiGroup: string; }

interface ResourceInstance {
  name: string;
  namespace?: string;
  controlPlaneContextRef: ControlPlaneContextRef;
}

function useKnownResourceTypes(): ResourceType[] {
  const { data } = useRelationshipsData();
  return useMemo(() => {
    if (!data) return [];
    const seen = new Map<string, ResourceType>();
    for (const r of data.items) {
      for (const ep of [r.spec.subject, r.spec.object]) {
        const key = `${ep.apiGroup}/${ep.kind}`;
        if (!seen.has(key)) seen.set(key, { kind: ep.kind, apiGroup: ep.apiGroup });
      }
    }
    return Array.from(seen.values()).sort((a, b) => a.kind.localeCompare(b.kind));
  }, [data]);
}

function useKnownInstances(kind: string, apiGroup: string): ResourceInstance[] {
  const { data } = useRelationshipsData();
  return useMemo(() => {
    if (!data || !kind) return [];
    const seen = new Map<string, ResourceInstance>();
    for (const r of data.items) {
      for (const ep of [r.spec.subject, r.spec.object]) {
        if (ep.kind !== kind || ep.apiGroup !== apiGroup) continue;
        const key = `${ep.namespace ?? ""}/${ep.name}`;
        if (!seen.has(key)) {
          seen.set(key, {
            name: ep.name,
            namespace: ep.namespace,
            controlPlaneContextRef: ep.controlPlaneContextRef,
          });
        }
      }
    }
    return Array.from(seen.values()).sort((a, b) => a.name.localeCompare(b.name));
  }, [data, kind, apiGroup]);
}

// ── constants ────────────────────────────────────────────────────────────────

const CONTEXT_KINDS: ControlPlaneContextKind[] = ["Platform", "Organization", "Project", "User"];
const DIRECTIONS: TraversalDirection[] = ["Outbound", "Inbound", "Both"];

// ── types ────────────────────────────────────────────────────────────────────

export interface SidebarFilters {
  selectedRelationshipTypes: Set<string>;
  selectedContextKinds: Set<ControlPlaneContextKind>;
}

interface Props {
  filters: SidebarFilters;
  onFiltersChange: (next: SidebarFilters) => void;
  history: QueryHistoryEntry[];
  onRunQuery: (spec: GraphQuerySpec) => void;
  isRunning: boolean;
}

// ── component ────────────────────────────────────────────────────────────────

export default function GraphSidebar({ filters, onFiltersChange, history, onRunQuery, isRunning }: Props) {
  const lastQuerySpec = useAppStore((s) => s.lastQuerySpec);
  const resourceTypes = useKnownResourceTypes();

  const [kind, setKind] = useState(lastQuerySpec?.root.kind ?? "");
  const [apiGroup, setApiGroup] = useState(lastQuerySpec?.root.apiGroup ?? "");
  const [namespace, setNamespace] = useState(lastQuerySpec?.root.namespace ?? "");
  const [name, setName] = useState(lastQuerySpec?.root.name ?? "");
  const [ctxKind, setCtxKind] = useState<ControlPlaneContextKind>(
    lastQuerySpec?.root.controlPlaneContextRef.kind ?? "Platform",
  );
  const [ctxName, setCtxName] = useState(lastQuerySpec?.root.controlPlaneContextRef.name ?? "");
  const [direction, setDirection] = useState<TraversalDirection>(
    lastQuerySpec?.traverse.direction ?? "Both",
  );
  const [maxDepth, setMaxDepth] = useState(lastQuerySpec?.traverse.maxDepth ?? 3);
  const [maxNodes, setMaxNodes] = useState(lastQuerySpec?.traverse.maxNodes ?? 500);

  const instances = useKnownInstances(kind, apiGroup);

  const namespaces = useMemo(() => {
    const ns = new Set(instances.map((i) => i.namespace ?? ""));
    return Array.from(ns).sort();
  }, [instances]);

  const isNamespaced = namespaces.some((ns) => ns !== "");

  const filteredInstances = useMemo(() => {
    if (!isNamespaced || !namespace) return instances;
    return instances.filter((i) => (i.namespace ?? "") === namespace);
  }, [instances, namespace, isNamespaced]);

  // Sync form when a history entry runs.
  useEffect(() => {
    if (!lastQuerySpec) return;
    setKind(lastQuerySpec.root.kind);
    setApiGroup(lastQuerySpec.root.apiGroup ?? "");
    setNamespace(lastQuerySpec.root.namespace ?? "");
    setName(lastQuerySpec.root.name);
    setCtxKind(lastQuerySpec.root.controlPlaneContextRef.kind);
    setCtxName(lastQuerySpec.root.controlPlaneContextRef.name);
    setDirection(lastQuerySpec.traverse.direction ?? "Both");
    setMaxDepth(lastQuerySpec.traverse.maxDepth ?? 3);
    setMaxNodes(lastQuerySpec.traverse.maxNodes ?? 500);
  }, [lastQuerySpec]);

  // When type changes, reset downstream fields.
  const handleTypeSelect = (value: string) => {
    if (!value) { setKind(""); setApiGroup(""); setNamespace(""); setName(""); return; }
    const slashIdx = value.indexOf("/");
    const grp = value.slice(0, slashIdx);
    const k = value.slice(slashIdx + 1);
    setApiGroup(grp);
    setKind(k);
    setNamespace("");
    setName("");
    setCtxName("");
  };

  // When namespace changes, clear name if it's no longer valid.
  const handleNamespaceSelect = (value: string) => {
    setNamespace(value);
    setName("");
    setCtxName("");
  };

  // When name is selected, auto-fill context from the known instance.
  const handleNameSelect = (value: string) => {
    setName(value);
    const inst = filteredInstances.find((i) => i.name === value);
    if (inst) {
      setCtxKind(inst.controlPlaneContextRef.kind);
      setCtxName(inst.controlPlaneContextRef.name);
    }
  };

  const { data: rtList } = useQuery({
    queryKey: ["relationshiptypes"],
    queryFn: () =>
      apiClient.get<KubernetesList<RelationshipType>>(
        "/apis/knowledge.miloapis.com/v1alpha1/relationshiptypes",
      ),
  });
  const relationshipTypes = rtList?.items ?? [];

  const typeSelectValue = kind && apiGroup !== undefined ? `${apiGroup}/${kind}` : "";
  const canRun = kind.trim() && name.trim() && ctxName.trim();

  const handleRun = () => {
    if (!canRun) return;
    onRunQuery({
      root: {
        apiGroup: apiGroup.trim(),
        kind: kind.trim(),
        name: name.trim(),
        namespace: namespace.trim() || undefined,
        controlPlaneContextRef: { kind: ctxKind, name: ctxName.trim() },
      },
      traverse: { direction, maxDepth, maxNodes },
    });
  };

  const toggleType = (n: string) => {
    const next = new Set(filters.selectedRelationshipTypes);
    if (next.has(n)) next.delete(n); else next.add(n);
    onFiltersChange({ ...filters, selectedRelationshipTypes: next });
  };

  return (
    <aside className={styles.root}>
      {/* ── Query form ─────────────────────────────────── */}
      <div className={styles.section}>
        <div className={styles.label}>Root resource</div>

        <select className={styles.select} value={typeSelectValue} onChange={(e) => handleTypeSelect(e.target.value)}>
          <option value="">— select type —</option>
          {resourceTypes.map((t) => (
            <option key={`${t.apiGroup}/${t.kind}`} value={`${t.apiGroup}/${t.kind}`}>
              {t.kind}{t.apiGroup ? ` (${t.apiGroup})` : ""}
            </option>
          ))}
          {kind && !resourceTypes.find((t) => t.kind === kind && t.apiGroup === apiGroup) && (
            <option value={typeSelectValue}>{kind}{apiGroup ? ` (${apiGroup})` : ""}</option>
          )}
        </select>

        {isNamespaced && (
          <select className={styles.select} value={namespace} onChange={(e) => handleNamespaceSelect(e.target.value)}>
            <option value="">— namespace —</option>
            {namespaces.filter(Boolean).map((ns) => (
              <option key={ns} value={ns}>{ns}</option>
            ))}
          </select>
        )}

        <select
          className={styles.select}
          value={name}
          onChange={(e) => handleNameSelect(e.target.value)}
          disabled={!kind}
        >
          <option value="">— name —</option>
          {filteredInstances.map((i) => (
            <option key={`${i.namespace ?? ""}/${i.name}`} value={i.name}>{i.name}</option>
          ))}
          {name && !filteredInstances.find((i) => i.name === name) && (
            <option value={name}>{name}</option>
          )}
        </select>

        <div className={styles.row}>
          <select
            className={styles.select}
            value={ctxKind}
            onChange={(e) => setCtxKind(e.target.value as ControlPlaneContextKind)}
          >
            {CONTEXT_KINDS.map((k) => <option key={k} value={k}>{k}</option>)}
          </select>
          <input
            className={styles.input}
            placeholder="Context name"
            value={ctxName}
            onChange={(e) => setCtxName(e.target.value)}
          />
        </div>
      </div>

      {/* ── Traversal ──────────────────────────────────── */}
      <div className={styles.section}>
        <div className={styles.label}>Direction</div>
        <div className={styles.segmented}>
          {DIRECTIONS.map((d) => (
            <button
              key={d}
              className={`${styles.segmentedItem} ${direction === d ? styles.segmentedItemActive : ""}`}
              onClick={() => setDirection(d)}
            >
              {d}
            </button>
          ))}
        </div>
      </div>

      <div className={styles.section}>
        <div className={styles.sliderRow}>
          <span className={styles.label}>Max depth</span>
          <span className={styles.sliderVal}>{maxDepth}</span>
        </div>
        <input type="range" min={1} max={10} step={1} className={styles.slider}
          value={maxDepth} onChange={(e) => setMaxDepth(Number(e.target.value))} />
      </div>

      <div className={styles.section}>
        <div className={styles.sliderRow}>
          <span className={styles.label}>Max nodes</span>
          <span className={styles.sliderVal}>{maxNodes}</span>
        </div>
        <input type="range" min={50} max={1000} step={50} className={styles.slider}
          value={maxNodes} onChange={(e) => setMaxNodes(Number(e.target.value))} />
      </div>

      <button className={styles.runButton} onClick={handleRun} disabled={!canRun || isRunning}>
        {isRunning ? "Running…" : "Run Query"}
      </button>

      <div className={styles.divider} />

      {/* ── Viewport filters ───────────────────────────── */}
      <div className={styles.section}>
        <div className={styles.label}>Visible contexts</div>
        {CONTEXT_KINDS.map((k) => {
          const checked = filters.selectedContextKinds.has(k);
          return (
            <label key={k} className={styles.checkboxRow}>
              <Checkbox.Root className={styles.checkbox} checked={checked}
                onCheckedChange={(v) => {
                  const next = new Set(filters.selectedContextKinds);
                  if (v) next.add(k); else next.delete(k);
                  onFiltersChange({ ...filters, selectedContextKinds: next });
                }}>
                <Checkbox.Indicator className={styles.checkboxIndicator} />
              </Checkbox.Root>
              {k}
            </label>
          );
        })}
      </div>

      <div className={styles.section}>
        <div className={styles.label}>Relationship types</div>
        <div className={styles.checkboxList}>
          {relationshipTypes.length === 0 ? (
            <div className={styles.empty}>No types loaded</div>
          ) : (
            relationshipTypes.map((rt) => {
              const n = rt.metadata.name;
              const checked = filters.selectedRelationshipTypes.has(n);
              return (
                <label key={n} className={styles.checkboxRow}>
                  <Checkbox.Root className={styles.checkbox} checked={checked}
                    onCheckedChange={() => toggleType(n)}>
                    <Checkbox.Indicator className={styles.checkboxIndicator} />
                  </Checkbox.Root>
                  {rt.spec.displayName ?? n}
                </label>
              );
            })
          )}
        </div>
      </div>

      {/* ── Recent queries ─────────────────────────────── */}
      {history.length > 0 && (
        <div className={styles.section}>
          <div className={styles.label}>Recent queries</div>
          {history.map((h, i) => (
            <button key={i} className={styles.historyItem} onClick={() => onRunQuery(h.spec)}>
              {h.spec.root.kind}/{h.spec.root.name}
            </button>
          ))}
        </div>
      )}
    </aside>
  );
}
