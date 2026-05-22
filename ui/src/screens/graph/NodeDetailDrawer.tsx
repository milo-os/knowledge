import { useEffect, useMemo } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import * as Tabs from "@radix-ui/react-tabs";
import { useMutation } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { apiClient } from "../../api/client";
import type {
  GraphQuery,
  GraphQueryResult,
  GraphQuerySpec,
} from "../../api/resources/graphQuery";
import type { ControlPlaneContextKind } from "../../api/resources/resourceRelationship";
import type { GraphEdge, GraphNode } from "./graphModel";
import { makeNodeId } from "./graphModel";
import styles from "./NodeDetailDrawer.module.css";

interface Props {
  nodes: GraphNode[];
  links: GraphEdge[];
}

const ctxPillClass: Record<ControlPlaneContextKind, string> = {
  Platform: styles.contextPillPlatform ?? "",
  Organization: styles.contextPillOrganization ?? "",
  Project: styles.contextPillProject ?? "",
  User: styles.contextPillUser ?? "",
};

function parseNodeIdParam(value: string | null) {
  if (!value) return null;
  const parts = value.split("/");
  if (parts.length < 3) return null;
  const kind = parts[0] ?? "";
  const namespace = parts[1] ?? "";
  const name = parts.slice(2).join("/");
  return { kind, namespace, name };
}

export default function NodeDetailDrawer({ nodes, links }: Props) {
  const [searchParams, setSearchParams] = useSearchParams();
  const nodeParam = searchParams.get("node");
  const dirParam = (searchParams.get("dir") as "Outbound" | "Inbound" | null) ?? "Outbound";

  const open = nodeParam !== null;

  const parsed = parseNodeIdParam(nodeParam);
  const nodeId = parsed ? makeNodeId(parsed.kind, parsed.namespace, parsed.name) : null;
  const node = useMemo(
    () => (nodeId ? nodes.find((n) => n.id === nodeId) ?? null : null),
    [nodes, nodeId],
  );

  const fallback = useMutation({
    mutationFn: async (spec: GraphQuerySpec) => {
      return apiClient.post<GraphQuery>(
        "/apis/knowledge.miloapis.com/v1alpha1/graphqueries",
        {
          apiVersion: "knowledge.miloapis.com/v1alpha1",
          kind: "GraphQuery",
          spec,
        },
      );
    },
  });

  useEffect(() => {
    if (!open || node || !parsed) return;
    fallback.mutate({
      root: {
        apiGroup: "",
        kind: parsed.kind,
        name: parsed.name,
        namespace: parsed.namespace || undefined,
        controlPlaneContextRef: { kind: "Platform", name: "" },
      },
      traverse: { direction: "Both", maxDepth: 1 },
    });
  }, [open, node, parsed]);

  const fallbackResult = fallback.data?.status as GraphQueryResult | undefined;

  const effectiveOutbound: GraphEdge[] = node
    ? links.filter((l) => l.source === node.id)
    : [];
  const effectiveInbound: GraphEdge[] = node
    ? links.filter((l) => l.target === node.id)
    : [];

  const fallbackOutbound = fallbackResult
    ? fallbackResult.edges
        .filter((e) => {
          const subj = fallbackResult.nodes.find((n) => n.id === e.subjectNodeID);
          return (
            subj &&
            subj.endpoint.kind === parsed?.kind &&
            subj.endpoint.name === parsed.name
          );
        })
        .map((e) => {
          const obj = fallbackResult.nodes.find((n) => n.id === e.objectNodeID);
          return {
            id: e.id,
            relationshipType: e.relationshipType,
            peerName: obj?.endpoint.name ?? "?",
            peerKind: obj?.endpoint.kind ?? "?",
            peerNamespace: obj?.endpoint.namespace ?? "",
          };
        })
    : [];

  const closeDrawer = () => {
    const next = new URLSearchParams(searchParams);
    next.delete("node");
    next.delete("dir");
    setSearchParams(next);
  };

  const setDir = (d: string) => {
    const next = new URLSearchParams(searchParams);
    next.set("dir", d);
    setSearchParams(next);
  };

  const traverseTo = (kind: string, namespace: string, name: string) => {
    const next = new URLSearchParams(searchParams);
    next.set("node", `${kind}/${namespace}/${name}`);
    setSearchParams(next);
  };

  return (
    <Dialog.Root open={open} onOpenChange={(o) => !o && closeDrawer()}>
      <Dialog.Portal>
        <Dialog.Overlay className={styles.overlay} />
        <Dialog.Content className={styles.content} aria-describedby={undefined}>
          <div className={styles.header}>
            <Dialog.Title className={styles.title}>Resource Detail</Dialog.Title>
            <Dialog.Close asChild>
              <button className={styles.closeBtn} aria-label="Close">×</button>
            </Dialog.Close>
          </div>

          <div className={styles.body}>
            <div className={styles.identity}>
              <div className={styles.identityRow}>
                <span className={styles.pill}>
                  {node?.kind ?? parsed?.kind ?? "?"}
                </span>
                <span className={styles.name}>{node?.name ?? parsed?.name}</span>
              </div>
              <div className={styles.identityRow}>
                <span className={styles.muted}>
                  ns: {node?.namespace || parsed?.namespace || "(cluster)"}
                </span>
                {node ? (
                  <span
                    className={`${styles.pill} ${ctxPillClass[node.controlPlaneContextKind]}`}
                  >
                    {node.controlPlaneContextKind}: {node.controlPlaneContextName || "—"}
                  </span>
                ) : null}
                {node?.apiGroup ? (
                  <span className={styles.muted}>{node.apiGroup}</span>
                ) : null}
              </div>
            </div>

            <Tabs.Root value={dirParam} onValueChange={setDir}>
              <Tabs.List className={styles.tabsList}>
                <Tabs.Trigger className={styles.tabsTrigger} value="Outbound">
                  Outbound
                </Tabs.Trigger>
                <Tabs.Trigger className={styles.tabsTrigger} value="Inbound">
                  Inbound
                </Tabs.Trigger>
              </Tabs.List>

              <Tabs.Content value="Outbound">
                <RelationshipList
                  edges={effectiveOutbound}
                  nodes={nodes}
                  peerKey="target"
                  onTraverse={traverseTo}
                />
                {!node && fallbackOutbound.length > 0 ? (
                  <div className={styles.relList}>
                    {fallbackOutbound.map((r) => (
                      <div key={r.id} className={styles.relRow}>
                        <span className={styles.pill}>{r.relationshipType}</span>
                        <span className={styles.relName}>{r.peerKind}/{r.peerName}</span>
                        <button
                          className={styles.traverseBtn}
                          onClick={() => traverseTo(r.peerKind, r.peerNamespace, r.peerName)}
                        >
                          Traverse →
                        </button>
                      </div>
                    ))}
                  </div>
                ) : null}
              </Tabs.Content>

              <Tabs.Content value="Inbound">
                <RelationshipList
                  edges={effectiveInbound}
                  nodes={nodes}
                  peerKey="source"
                  onTraverse={traverseTo}
                />
              </Tabs.Content>
            </Tabs.Root>

            {!node && fallback.isPending ? (
              <div className={styles.empty}>Loading node…</div>
            ) : null}
          </div>

          <div className={styles.footer} />
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

interface RelationshipListProps {
  edges: GraphEdge[];
  nodes: GraphNode[];
  peerKey: "source" | "target";
  onTraverse: (kind: string, namespace: string, name: string) => void;
}

function RelationshipList({ edges, nodes, peerKey, onTraverse }: RelationshipListProps) {
  const byId = useMemo(() => {
    const m = new Map<string, GraphNode>();
    for (const n of nodes) m.set(n.id, n);
    return m;
  }, [nodes]);

  if (edges.length === 0) {
    return <div className={styles.empty}>No relationships</div>;
  }

  return (
    <div className={styles.relList}>
      {edges.map((e) => {
        const peerId = peerKey === "source" ? e.source : e.target;
        const peer = byId.get(peerId);
        const sourceLabel: "Policy" | "Manual" = "Policy";
        const badgeClass =
          sourceLabel === "Policy" ? styles.sourceBadgePolicy : styles.sourceBadgeManual;
        return (
          <div key={e.id} className={styles.relRow}>
            <span className={styles.pill}>{e.relationshipType}</span>
            <span className={styles.relName}>
              {peer ? `${peer.kind}/${peer.name}` : peerId}
            </span>
            <span className={`${styles.pill} ${badgeClass}`}>{sourceLabel}</span>
            <button
              className={styles.traverseBtn}
              disabled={!peer}
              onClick={() => peer && onTraverse(peer.kind, peer.namespace, peer.name)}
            >
              Traverse →
            </button>
          </div>
        );
      })}
    </div>
  );
}
