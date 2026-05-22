import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { apiClient, type KubernetesList } from "../../api/client";
import type { ResourceRelationship } from "../../api/resources/resourceRelationship";
import { useWatch } from "../../hooks/useWatch";
import InventoryFilters from "./InventoryFilters";
import RelationshipTable from "./RelationshipTable";
import TableFooter from "./TableFooter";
import styles from "./inventory.module.css";

const BASE_PATH = "/apis/knowledge.miloapis.com/v1alpha1/resourcerelationships";

export default function RelationshipInventoryPage() {
  const [params] = useSearchParams();

  const type = params.get("type") ?? "";
  const kind = params.get("kind") ?? "";
  const source = params.get("source") ?? "";
  const validity = params.get("validity") ?? "";
  const context = params.get("context") ?? "";
  const q = params.get("q") ?? "";
  const page = params.get("page") ?? "";

  const resourcePath = useMemo(() => {
    const labelParts: string[] = [];
    if (type) labelParts.push(`knowledge.miloapis.com/relationship-type=${type}`);
    if (source) labelParts.push(`knowledge.miloapis.com/source-type=${source}`);
    if (kind) labelParts.push(`knowledge.miloapis.com/subject-kind=${kind}`);

    const qs = new URLSearchParams();
    qs.set("limit", "50");
    if (page) qs.set("continue", page);
    if (labelParts.length > 0) qs.set("labelSelector", labelParts.join(","));
    return `${BASE_PATH}?${qs.toString()}`;
  }, [type, source, kind, page]);

  const queryKey = [
    "knowledge",
    "v1alpha1",
    "resourcerelationships",
    { type, source, kind, validity, context, q, page },
  ] as const;

  const { data, isLoading, error } = useQuery({
    queryKey,
    queryFn: () => apiClient.get<KubernetesList<ResourceRelationship>>(resourcePath),
  });

  useWatch<ResourceRelationship>({
    resourcePath,
    queryKey,
    enabled: !!data,
  });

  const items = data?.items ?? [];

  const filtered = useMemo(() => {
    return items.filter((r) => {
      if (validity === "Valid") {
        const vc = r.status?.conditions?.find(c => c.type === "Valid");
        if (vc?.status !== "True") return false;
      }
      if (validity === "Invalid") {
        const vc = r.status?.conditions?.find(c => c.type === "Valid");
        if (vc?.status !== "False") return false;
      }
      if (
        context &&
        r.spec.subject.controlPlaneContextRef.kind !== context &&
        r.spec.object.controlPlaneContextRef.kind !== context
      ) {
        return false;
      }
      if (q) {
        const needle = q.toLowerCase();
        const hay = [
          r.metadata.name,
          r.spec.relationshipType.name,
          r.spec.subject.kind,
          r.spec.subject.name,
          r.spec.object.kind,
          r.spec.object.name,
        ]
          .join(" ")
          .toLowerCase();
        if (!hay.includes(needle)) return false;
      }
      return true;
    });
  }, [items, validity, context, q]);

  const onExport = () => {
    const blob = new Blob([JSON.stringify(filtered, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `relationships-${Date.now()}.json`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div>
          <h1 className={styles.title}>Relationships</h1>
          <p className={styles.subtitle}>
            All resource relationships discovered by policies or created manually.
          </p>
        </div>
        <button className={styles.export} onClick={onExport} disabled={filtered.length === 0}>
          Export JSON
        </button>
      </div>

      <InventoryFilters />

      {error ? (
        <div className={styles.error}>Failed to load relationships: {(error as Error).message}</div>
      ) : isLoading ? (
        <div className={styles.loading}>Loading…</div>
      ) : (
        <>
          <RelationshipTable items={filtered} />
          <TableFooter
            shown={filtered.length}
            remaining={data?.metadata.remainingItemCount}
            continueToken={data?.metadata.continue}
          />
        </>
      )}
    </div>
  );
}
