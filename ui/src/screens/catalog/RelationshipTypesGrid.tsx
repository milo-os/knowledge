import { useQuery } from "@tanstack/react-query";
import { apiClient, type KubernetesList } from "../../api/client";
import type { RelationshipType } from "../../api/resources/relationshipType";
import { useWatch } from "../../hooks/useWatch";
import TypeCard from "./TypeCard";
import styles from "./RelationshipTypesGrid.module.css";

const RESOURCE_PATH = "/apis/knowledge.miloapis.com/v1alpha1/relationshiptypes";
const QUERY_KEY = ["knowledge", "v1alpha1", "relationshiptypes"] as const;

export default function RelationshipTypesGrid() {
  const { data, isLoading, error } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: () => apiClient.get<KubernetesList<RelationshipType>>(RESOURCE_PATH),
  });

  useWatch<RelationshipType>({
    resourcePath: RESOURCE_PATH,
    queryKey: QUERY_KEY,
  });

  if (isLoading) return <div className={styles.muted}>Loading types…</div>;
  if (error)
    return <div className={styles.error}>Failed to load relationship types</div>;

  const items = data?.items ?? [];
  if (items.length === 0) {
    return (
      <div className={styles.empty}>
        No relationship types yet. Click "Create Type" to add one.
      </div>
    );
  }

  return (
    <div className={styles.grid}>
      {items.map((t) => (
        <TypeCard key={t.metadata.name} item={t} />
      ))}
    </div>
  );
}
