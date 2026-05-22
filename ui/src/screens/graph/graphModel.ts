import type { GraphQueryResult } from "../../api/resources/graphQuery";
import type { ControlPlaneContextKind } from "../../api/resources/resourceRelationship";
import type { RelationshipType } from "../../api/resources/relationshipType";

export interface GraphNode {
  id: string;
  kind: string;
  name: string;
  namespace: string;
  apiGroup: string;
  controlPlaneContextKind: ControlPlaneContextKind;
  controlPlaneContextName: string;
  depth: number;
  x?: number;
  y?: number;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  relationshipType: string;
  direction: "Outbound" | "Inbound";
}

export function makeNodeId(kind: string, namespace: string, name: string): string {
  return `${kind}/${namespace}/${name}`;
}

export function mapGraphQueryResultToCanvasModel(
  result: GraphQueryResult,
  rootId?: string,
  relationshipTypes?: RelationshipType[],
): { nodes: GraphNode[]; links: GraphEdge[] } {
  const nodes: GraphNode[] = result.nodes.map((n) => {
    const ep = n.endpoint;
    return {
      id: makeNodeId(ep.kind, ep.namespace ?? "", ep.name),
      kind: ep.kind,
      name: ep.name,
      namespace: ep.namespace ?? "",
      apiGroup: ep.apiGroup,
      controlPlaneContextKind: ep.controlPlaneContextRef.kind,
      controlPlaneContextName: ep.controlPlaneContextRef.name,
      depth: n.depth,
    };
  });

  const nodeById = new Map<string, GraphNode>();
  for (const n of nodes) nodeById.set(n.id, n);

  const rawIdToNodeId = new Map<string, string>();
  for (let i = 0; i < result.nodes.length; i++) {
    const raw = result.nodes[i];
    const mapped = nodes[i];
    if (raw && mapped) rawIdToNodeId.set(raw.id, mapped.id);
  }

  const links: GraphEdge[] = result.edges.map((e) => {
    const source = rawIdToNodeId.get(e.subjectNodeID) ?? e.subjectNodeID;
    const target = rawIdToNodeId.get(e.objectNodeID) ?? e.objectNodeID;
    const direction: "Outbound" | "Inbound" =
      rootId && target === rootId ? "Inbound" : "Outbound";
    return {
      id: e.id,
      source,
      target,
      relationshipType: e.relationshipType,
      direction,
    };
  });

  if (!relationshipTypes?.length) return { nodes, links };

  // Build set of transparent subject kinds (kind only — apiGroup not encoded in node IDs).
  const transparentKinds = new Set<string>(
    relationshipTypes
      .filter((rt) => rt.spec.transparent)
      .map((rt) => rt.spec.subjectGVK.kind),
  );

  if (transparentKinds.size === 0) return { nodes, links };

  return collapseTransparentNodes(nodes, links, transparentKinds, rootId);
}

function collapseTransparentNodes(
  nodes: GraphNode[],
  links: GraphEdge[],
  transparentKinds: Set<string>,
  rootId?: string,
): { nodes: GraphNode[]; links: GraphEdge[] } {
  const bridgeIds = new Set(
    nodes.filter((n) => transparentKinds.has(n.kind)).map((n) => n.id),
  );

  if (bridgeIds.size === 0) return { nodes, links };

  // For each bridge node, collect all its neighbour node IDs (across all edges).
  const neighbours = new Map<string, Set<string>>();
  for (const id of bridgeIds) neighbours.set(id, new Set());

  for (const l of links) {
    if (bridgeIds.has(l.source)) neighbours.get(l.source)!.add(l.target);
    if (bridgeIds.has(l.target)) neighbours.get(l.target)!.add(l.source);
  }

  const syntheticLinks: GraphEdge[] = [];
  const collapsedBridgeIds = new Set<string>();

  for (const [bridgeId, nbrs] of neighbours) {
    const nbrList = Array.from(nbrs).filter((id) => !bridgeIds.has(id));
    // Only collapse when the bridge has exactly two non-bridge neighbours.
    if (nbrList.length !== 2) continue;

    collapsedBridgeIds.add(bridgeId);
    const [a, b] = nbrList as [string, string];
    const direction: "Outbound" | "Inbound" =
      rootId && b === rootId ? "Inbound" : "Outbound";

    // Derive a label from the relationship types of the two hops.
    const hopTypes = links
      .filter(
        (l) =>
          (l.source === bridgeId || l.target === bridgeId) &&
          (l.source === a || l.target === a || l.source === b || l.target === b),
      )
      .map((l) => l.relationshipType)
      .filter(Boolean);
    const label = hopTypes.length > 0 ? hopTypes[0]! : "related-to";

    syntheticLinks.push({
      id: `synthetic-${bridgeId}`,
      source: a,
      target: b,
      relationshipType: label,
      direction,
    });
  }

  const keptNodes = nodes.filter((n) => !collapsedBridgeIds.has(n.id));
  const keptLinks = links.filter(
    (l) => !collapsedBridgeIds.has(l.source) && !collapsedBridgeIds.has(l.target),
  );

  return { nodes: keptNodes, links: [...keptLinks, ...syntheticLinks] };
}
