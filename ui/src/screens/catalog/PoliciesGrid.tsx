import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { apiClient, type KubernetesList } from "../../api/client";
import type { RelationshipPolicy } from "../../api/resources/relationshipPolicy";
import { useWatch } from "../../hooks/useWatch";
import { useAppStore } from "../../state/store";
import PolicyCard from "./PolicyCard";
import styles from "./PoliciesGrid.module.css";

export default function PoliciesGrid() {
  const navigate = useNavigate();
  const ctx = useAppStore((s) => s.currentContext);
  const namespace = ctx?.name ?? "default";
  const resourcePath = `/apis/knowledge.miloapis.com/v1alpha1/namespaces/${namespace}/relationshippolicies`;
  const queryKey = ["knowledge", "v1alpha1", "relationshippolicies", namespace] as const;

  const { data, isLoading, error } = useQuery({
    queryKey,
    queryFn: () => apiClient.get<KubernetesList<RelationshipPolicy>>(resourcePath),
  });

  useWatch<RelationshipPolicy>({ resourcePath, queryKey });

  const items = data?.items ?? [];

  return (
    <div className={styles.wrapper}>
      <div className={styles.toolbar}>
        <span className={styles.nsLabel}>
          Namespace: <span className={styles.ns}>{namespace}</span>
        </span>
        <button
          type="button"
          className={styles.createBtn}
          onClick={() => navigate("/catalog/policies/new")}
        >
          Create Policy
        </button>
      </div>

      {isLoading && <div className={styles.muted}>Loading policies…</div>}
      {error && (
        <div className={styles.errorBox}>Failed to load policies</div>
      )}
      {!isLoading && !error && items.length === 0 && (
        <div className={styles.empty}>
          No discovery policies in this namespace.
        </div>
      )}

      {items.length > 0 && (
        <div className={styles.grid}>
          {items.map((p) => (
            <PolicyCard key={p.metadata.name} item={p} />
          ))}
        </div>
      )}
    </div>
  );
}
