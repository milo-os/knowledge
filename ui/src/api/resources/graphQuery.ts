import type {
  ControlPlaneContextRef,
  ObjectMeta,
  ResourceRelationshipEndpoint,
} from "./resourceRelationship";

export type TraversalDirection = "Outbound" | "Inbound" | "Both";

export interface GraphQueryRoot {
  apiGroup: string;
  kind: string;
  name: string;
  namespace?: string;
  controlPlaneContextRef: ControlPlaneContextRef;
}

export interface GraphQueryTraverse {
  relationshipTypes?: string[];
  direction?: TraversalDirection;
  maxDepth?: number;
  maxNodes?: number;
}

export interface GraphQuerySpec {
  root: GraphQueryRoot;
  traverse: GraphQueryTraverse;
}

export interface GraphNode {
  id: string;
  endpoint: ResourceRelationshipEndpoint;
  depth: number;
}

export interface GraphEdge {
  id: string;
  relationshipType: string;
  subjectNodeID: string;
  objectNodeID: string;
}

export interface GraphQueryResult {
  nodes: GraphNode[];
  edges: GraphEdge[];
  status: { truncated: boolean };
}

export interface GraphQuery {
  apiVersion: "knowledge.miloapis.com/v1alpha1";
  kind: "GraphQuery";
  metadata?: ObjectMeta;
  spec: GraphQuerySpec;
  status?: {
    nodes?: GraphNode[];
    edges?: GraphEdge[];
    truncated?: boolean;
  };
}
