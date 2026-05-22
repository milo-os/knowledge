import type {
  Condition,
  ControlPlaneContextRef,
  ObjectMeta,
} from "./resourceRelationship";

export interface SubjectSelector {
  apiGroup: string;
  kind: string;
  namespaces?: string[];
}

export interface RelationshipPolicySpec {
  relationshipType: { name: string };
  subject: SubjectSelector;
  expression: string;
  controlPlaneContextRef: ControlPlaneContextRef;
}

export interface RelationshipPolicyStatus {
  observedGeneration?: number;
  discoveredEdgesCount?: number;
  conditions?: Condition[];
}

export interface RelationshipPolicy {
  apiVersion: "knowledge.miloapis.com/v1alpha1";
  kind: "RelationshipPolicy";
  metadata: ObjectMeta;
  spec: RelationshipPolicySpec;
  status?: RelationshipPolicyStatus;
}
