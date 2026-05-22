import type { Condition, ObjectMeta } from "./resourceRelationship";

export interface GroupVersionKind {
  group: string;
  version: string;
  kind: string;
}

export type Cardinality = "OneToOne" | "OneToMany" | "ManyToMany";

export interface RelationshipTypeSpec {
  displayName?: string;
  description?: string;
  subjectGVK: GroupVersionKind;
  objectGVK: GroupVersionKind;
  cardinality: Cardinality;
  transparent?: boolean;
}

export interface RelationshipTypeStatus {
  observedGeneration?: number;
  conditions?: Condition[];
}

export interface RelationshipType {
  apiVersion: "knowledge.miloapis.com/v1alpha1";
  kind: "RelationshipType";
  metadata: ObjectMeta;
  spec: RelationshipTypeSpec;
  status?: RelationshipTypeStatus;
}
