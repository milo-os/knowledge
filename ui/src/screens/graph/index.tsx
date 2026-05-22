import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { apiClient } from "../../api/client";
import type {
  GraphQuery,
  GraphQueryResult,
  GraphQuerySpec,
} from "../../api/resources/graphQuery";
import type { ControlPlaneContextKind } from "../../api/resources/resourceRelationship";
import type { RelationshipType } from "../../api/resources/relationshipType";
import type { KubernetesList } from "../../api/client";
import { useAppStore } from "../../state/store";
import GraphSidebar, { type SidebarFilters } from "./GraphSidebar";
import GraphCanvas from "./GraphCanvas";
import GraphTopBar from "./GraphTopBar";
import NodeDetailDrawer from "./NodeDetailDrawer";
import TruncationBanner from "./TruncationBanner";
import {
  makeNodeId,
  mapGraphQueryResultToCanvasModel,
  type GraphEdge,
  type GraphNode,
} from "./graphModel";
import { loadQueryHistory, pushQueryHistory, type QueryHistoryEntry } from "./queryHistory";
import styles from "./index.module.css";

const ALL_CONTEXTS: ControlPlaneContextKind[] = [
  "Platform",
  "Organization",
  "Project",
  "User",
];

export default function GraphExplorerPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const lastQuerySpec = useAppStore((s) => s.lastQuerySpec);
  const setLastQuerySpec = useAppStore((s) => s.setLastQuerySpec);

  const queryClient = useQueryClient();
  const fitViewRef = useRef<() => void>(() => {});

  const [filters, setFilters] = useState<SidebarFilters>({
    selectedRelationshipTypes: new Set<string>(),
    selectedContextKinds: new Set<ControlPlaneContextKind>(ALL_CONTEXTS),
  });
  const [history, setHistory] = useState<QueryHistoryEntry[]>(() => loadQueryHistory());

  const queryMutation = useMutation({
    mutationFn: async (spec: GraphQuerySpec) => {
      const ns = spec.root.namespace || "default";
      const created = await apiClient.post<GraphQuery>(
        `/apis/knowledge.miloapis.com/v1alpha1/namespaces/${encodeURIComponent(ns)}/graphqueries`,
        { apiVersion: "knowledge.miloapis.com/v1alpha1", kind: "GraphQuery", spec },
      );
      const result: GraphQueryResult = {
        nodes: created.status?.nodes ?? [],
        edges: created.status?.edges ?? [],
        status: { truncated: created.status?.truncated ?? false },
      };
      return { spec, result };
    },
    onSuccess: ({ spec, result }) => {
      queryClient.setQueryData(["graphquery", JSON.stringify(spec)], result);
      setLastQuerySpec(spec);
    },
  });

  const result = lastQuerySpec
    ? (queryClient.getQueryData<GraphQueryResult>(["graphquery", JSON.stringify(lastQuerySpec)]) ?? null)
    : null;

  const { data: rtList } = useQuery({
    queryKey: ["relationshiptypes"],
    queryFn: () =>
      apiClient.get<KubernetesList<RelationshipType>>(
        "/apis/knowledge.miloapis.com/v1alpha1/relationshiptypes",
      ),
    staleTime: 60_000,
  });
  const relationshipTypes = rtList?.items ?? [];

  useEffect(() => {
    if (lastQuerySpec) {
      setHistory(pushQueryHistory(lastQuerySpec));
    }
  }, [lastQuerySpec]);

  const rootId = useMemo(() => {
    const root = lastQuerySpec?.root;
    if (!root) return undefined;
    return makeNodeId(root.kind, root.namespace ?? "", root.name);
  }, [lastQuerySpec]);

  const { nodes: allNodes, links: allLinks } = useMemo(() => {
    if (!result) return { nodes: [] as GraphNode[], links: [] as GraphEdge[] };
    return mapGraphQueryResultToCanvasModel(result, rootId, relationshipTypes);
  }, [result, rootId, relationshipTypes]);

  const visible = useMemo(() => {
    const nodes = allNodes.filter((n) =>
      filters.selectedContextKinds.has(n.controlPlaneContextKind),
    );
    const visibleIds = new Set(nodes.map((n) => n.id));
    const links = allLinks.filter((l) => {
      if (!visibleIds.has(l.source) || !visibleIds.has(l.target)) return false;
      if (
        filters.selectedRelationshipTypes.size > 0 &&
        !filters.selectedRelationshipTypes.has(l.relationshipType)
      )
        return false;
      return true;
    });
    return { nodes, links };
  }, [allNodes, allLinks, filters]);

  const depthTruncated = useMemo(() => {
    if (!result || !lastQuerySpec?.traverse.maxDepth) return false;
    const maxDepth = lastQuerySpec.traverse.maxDepth;
    const present = new Set(allNodes.map((n) => n.id));
    const byId = new Map(allNodes.map((n) => [n.id, n] as const));
    for (const e of allLinks) {
      const sNode = byId.get(e.source);
      const tNode = byId.get(e.target);
      if (sNode && sNode.depth === maxDepth && !present.has(e.target)) return true;
      if (tNode && tNode.depth === maxDepth && !present.has(e.source)) return true;
    }
    return false;
  }, [result, allNodes, allLinks, lastQuerySpec]);

  const nodeParam = searchParams.get("node");
  const selectedNodeId = nodeParam;

  const handleNodeClick = (id: string) => {
    setSearchParams((prev) => {
      const p = new URLSearchParams(prev);
      p.set("node", id);
      return p;
    });
  };

  return (
    <div className={styles.root}>
      <GraphSidebar
        filters={filters}
        onFiltersChange={setFilters}
        history={history}
        onRunQuery={(spec) => queryMutation.mutate(spec)}
        isRunning={queryMutation.isPending}
      />
      <div className={styles.main}>
        <GraphTopBar onFit={() => fitViewRef.current()} />
        <TruncationBanner
          serverTruncated={result?.status.truncated ?? false}
          depthTruncated={depthTruncated}
        />
        <div className={styles.canvasArea}>
          <GraphCanvas
            nodes={visible.nodes}
            links={visible.links}
            selectedNodeId={selectedNodeId}
            onNodeClick={handleNodeClick}
            onRegisterFit={(fn) => { fitViewRef.current = fn; }}
          />
        </div>
      </div>

      <NodeDetailDrawer nodes={allNodes} links={allLinks} />
    </div>
  );
}
