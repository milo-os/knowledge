import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { apiClient, type KubernetesList } from "../../api/client";
import type { RelationshipType } from "../../api/resources/relationshipType";
import type { ResourceRelationship } from "../../api/resources/resourceRelationship";
import type { RelationshipPolicy } from "../../api/resources/relationshipPolicy";
import styles from "./TypeCard.module.css";

interface Props {
  item: RelationshipType;
}

function edgeCountPath(name: string, limit: number) {
  const selector = encodeURIComponent(
    `knowledge.miloapis.com/relationship-type=${name}`,
  );
  return `/apis/knowledge.miloapis.com/v1alpha1/resourcerelationships?labelSelector=${selector}&limit=${limit}`;
}

export default function TypeCard({ item }: Props) {
  const navigate = useNavigate();
  const [expanded, setExpanded] = useState(false);
  const name = item.metadata.name;

  const edgeCountQuery = useQuery({
    queryKey: ["knowledge", "edgeCount", name],
    queryFn: () =>
      apiClient.get<KubernetesList<ResourceRelationship>>(
        edgeCountPath(name, 1),
      ),
    staleTime: 60_000,
  });

  const edgeCount = (() => {
    const list = edgeCountQuery.data;
    if (!list) return null;
    const remaining = list.metadata.remainingItemCount ?? 0;
    return list.items.length + remaining;
  })();

  const samplesQuery = useQuery({
    queryKey: ["knowledge", "typeSamples", name],
    queryFn: () =>
      apiClient.get<KubernetesList<ResourceRelationship>>(
        edgeCountPath(name, 3),
      ),
    enabled: expanded,
    staleTime: 60_000,
  });

  const policiesQuery = useQuery({
    queryKey: ["knowledge", "typePolicies", name],
    queryFn: () =>
      apiClient.get<KubernetesList<RelationshipPolicy>>(
        "/apis/knowledge.miloapis.com/v1alpha1/relationshippolicies",
      ),
    enabled: expanded,
    staleTime: 60_000,
  });

  const matchingPolicies = (policiesQuery.data?.items ?? []).filter(
    (p) => p.spec.relationshipType?.name === name,
  );

  const onChipClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigate(`/relationships?type=${encodeURIComponent(name)}`);
  };

  return (
    <div
      className={styles.card}
      onClick={() => setExpanded((v) => !v)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          setExpanded((v) => !v);
        }
      }}
    >
      <div className={styles.headerRow}>
        <div className={styles.name}>{name}</div>
        <span className={styles.cardinality}>{item.spec.cardinality}</span>
      </div>

      {item.spec.displayName && (
        <div className={styles.displayName}>{item.spec.displayName}</div>
      )}

      <div className={styles.direction}>
        <span className={styles.kind}>{item.spec.subjectGVK.kind}</span>
        <span className={styles.arrow}>→</span>
        <span className={styles.kind}>{item.spec.objectGVK.kind}</span>
      </div>

      <div className={styles.footerRow}>
        <button
          type="button"
          className={styles.edgeChip}
          onClick={onChipClick}
          disabled={edgeCount === null}
        >
          {edgeCount === null ? "…" : `${edgeCount} edges`}
        </button>
      </div>

      {expanded && (
        <div
          className={styles.detail}
          onClick={(e) => e.stopPropagation()}
        >
          <div className={styles.detailSection}>
            <div className={styles.detailLabel}>Sample edges</div>
            {samplesQuery.isLoading && (
              <div className={styles.muted}>Loading…</div>
            )}
            {!samplesQuery.isLoading &&
              (samplesQuery.data?.items ?? []).length === 0 && (
                <div className={styles.muted}>None</div>
              )}
            <ul className={styles.list}>
              {(samplesQuery.data?.items ?? []).slice(0, 3).map((r) => (
                <li key={r.metadata.uid ?? r.metadata.name} className={styles.listItem}>
                  <span className={styles.endpoint}>
                    {r.spec.subject.kind}/{r.spec.subject.name}
                  </span>
                  <span className={styles.arrow}>→</span>
                  <span className={styles.endpoint}>
                    {r.spec.object.kind}/{r.spec.object.name}
                  </span>
                </li>
              ))}
            </ul>
          </div>
          <div className={styles.detailSection}>
            <div className={styles.detailLabel}>Producing policies</div>
            {policiesQuery.isLoading && (
              <div className={styles.muted}>Loading…</div>
            )}
            {!policiesQuery.isLoading && matchingPolicies.length === 0 && (
              <div className={styles.muted}>None</div>
            )}
            <ul className={styles.list}>
              {matchingPolicies.map((p) => (
                <li key={`${p.metadata.namespace}/${p.metadata.name}`} className={styles.listItem}>
                  {p.metadata.namespace}/{p.metadata.name}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}
