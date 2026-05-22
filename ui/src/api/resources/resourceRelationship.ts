export type ControlPlaneContextKind =
  | "Platform"
  | "Organization"
  | "Project"
  | "User";

export interface ControlPlaneContextRef {
  kind: ControlPlaneContextKind;
  name: string;
}

export interface ResourceRelationshipEndpoint {
  apiGroup: string;
  kind: string;
  name: string;
  namespace?: string;
  controlPlaneContextRef: ControlPlaneContextRef;
}

export interface ObjectReference {
  apiVersion?: string;
  kind?: string;
  name?: string;
  namespace?: string;
  uid?: string;
}

export interface RelationshipSource {
  type: "Manual" | "Policy";
  policyRef?: ObjectReference;
}

export interface Condition {
  type: string;
  status: "True" | "False" | "Unknown";
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
  observedGeneration?: number;
}

export interface ObjectMeta {
  name: string;
  namespace?: string;
  uid?: string;
  resourceVersion?: string;
  generation?: number;
  creationTimestamp?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface ResourceRelationshipSpec {
  relationshipType: { name: string };
  subject: ResourceRelationshipEndpoint;
  object: ResourceRelationshipEndpoint;
  source: RelationshipSource;
}

export interface ResourceRelationshipStatus {
  observedGeneration?: number;
  conditions?: Condition[];
}

export interface ResourceRelationship {
  apiVersion: "knowledge.miloapis.com/v1alpha1";
  kind: "ResourceRelationship";
  metadata: ObjectMeta;
  spec: ResourceRelationshipSpec;
  status?: ResourceRelationshipStatus;
}
