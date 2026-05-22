import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { apiClient, type KubernetesList } from "../../api/client";
import type { RelationshipType } from "../../api/resources/relationshipType";
import styles from "./InventoryFilters.module.css";

const CONTEXT_OPTIONS = ["Platform", "Organization", "Project", "User"] as const;
const SOURCE_OPTIONS = ["Policy", "Manual"] as const;
const VALIDITY_OPTIONS = ["Valid", "Invalid"] as const;

export default function InventoryFilters() {
  const [params, setParams] = useSearchParams();

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    next.delete("page");
    setParams(next, { replace: true });
  };

  const q = params.get("q") ?? "";
  const [qDraft, setQDraft] = useState(q);

  useEffect(() => {
    setQDraft(q);
  }, [q]);

  useEffect(() => {
    const t = setTimeout(() => {
      if (qDraft !== q) setParam("q", qDraft);
    }, 300);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qDraft]);

  const { data: types } = useQuery({
    queryKey: ["knowledge", "v1alpha1", "relationshiptypes"],
    queryFn: () =>
      apiClient.get<KubernetesList<RelationshipType>>(
        "/apis/knowledge.miloapis.com/v1alpha1/relationshiptypes",
      ),
    staleTime: 60_000,
  });

  return (
    <div className={styles.root} role="search">
      <input
        className={styles.search}
        placeholder="Search relationships…"
        value={qDraft}
        onChange={(e) => setQDraft(e.target.value)}
        aria-label="Search"
      />

      <label className={styles.label}>
        Type
        <select
          className={styles.select}
          value={params.get("type") ?? ""}
          onChange={(e) => setParam("type", e.target.value)}
        >
          <option value="">All</option>
          {types?.items.map((t) => (
            <option key={t.metadata.name} value={t.metadata.name}>
              {t.spec.displayName ?? t.metadata.name}
            </option>
          ))}
        </select>
      </label>

      <label className={styles.label}>
        Context
        <select
          className={styles.select}
          value={params.get("context") ?? ""}
          onChange={(e) => setParam("context", e.target.value)}
        >
          <option value="">All</option>
          {CONTEXT_OPTIONS.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
      </label>

      <label className={styles.label}>
        Source
        <select
          className={styles.select}
          value={params.get("source") ?? ""}
          onChange={(e) => setParam("source", e.target.value)}
        >
          <option value="">All</option>
          {SOURCE_OPTIONS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </label>

      <label className={styles.label}>
        Validity
        <select
          className={styles.select}
          value={params.get("validity") ?? ""}
          onChange={(e) => setParam("validity", e.target.value)}
        >
          <option value="">All</option>
          {VALIDITY_OPTIONS.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}
