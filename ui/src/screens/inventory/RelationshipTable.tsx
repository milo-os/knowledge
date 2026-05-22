import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import type { ResourceRelationship } from "../../api/resources/resourceRelationship";
import styles from "./RelationshipTable.module.css";

type SortKey = "type" | "from" | "to" | "source" | "valid" | "created";
type SortDir = "asc" | "desc";

interface Props {
  items: ResourceRelationship[];
}

const COLUMNS: { key: SortKey; label: string }[] = [
  { key: "type", label: "Type" },
  { key: "from", label: "From" },
  { key: "to", label: "To" },
  { key: "source", label: "Source" },
  { key: "valid", label: "Valid" },
  { key: "created", label: "Created" },
];

export default function RelationshipTable({ items }: Props) {
  const navigate = useNavigate();
  const [sortKey, setSortKey] = useState<SortKey>("created");
  const [sortDir, setSortDir] = useState<SortDir>("desc");

  const sorted = useMemo(() => {
    const out = [...items];
    out.sort((a, b) => {
      const av = sortValue(a, sortKey);
      const bv = sortValue(b, sortKey);
      const cmp = av < bv ? -1 : av > bv ? 1 : 0;
      return sortDir === "asc" ? cmp : -cmp;
    });
    return out;
  }, [items, sortKey, sortDir]);

  const onHeader = (key: SortKey) => {
    if (key === sortKey) {
      setSortDir(sortDir === "asc" ? "desc" : "asc");
    } else {
      setSortKey(key);
      setSortDir("asc");
    }
  };

  const onRow = (r: ResourceRelationship) => {
    const ep = r.spec.subject;
    navigate(
      `/graph?node=${encodeURIComponent(ep.kind)}/${encodeURIComponent(ep.namespace ?? "")}/${encodeURIComponent(ep.name)}`,
    );
  };

  if (sorted.length === 0) {
    return (
      <div className={styles.wrap}>
        <div className={styles.empty}>No relationships match the current filters.</div>
      </div>
    );
  }

  return (
    <div className={styles.wrap}>
      <table className={styles.table}>
        <thead className={styles.thead}>
          <tr>
            {COLUMNS.map((c) => (
              <th
                key={c.key}
                className={styles.th}
                onClick={() => onHeader(c.key)}
                title="Sorted within current page only"
              >
                {c.label}
                {sortKey === c.key && (
                  <span className={styles.sortIndicator}>
                    {sortDir === "asc" ? "▲" : "▼"}
                  </span>
                )}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sorted.map((r) => (
            <tr
              key={r.metadata.uid ?? `${r.metadata.namespace}/${r.metadata.name}`}
              className={styles.row}
              onClick={() => onRow(r)}
            >
              <td className={styles.td}>
                <span className={styles.pill}>{r.spec.relationshipType.name}</span>
              </td>
              <td className={styles.td}>
                <Endpoint kind={r.spec.subject.kind} name={r.spec.subject.name} />
              </td>
              <td className={styles.td}>
                <Endpoint kind={r.spec.object.kind} name={r.spec.object.name} />
              </td>
              <td className={styles.td}>
                {r.spec.source.type === "Policy" ? (
                  <span className={`${styles.badge} ${styles.badgePolicy}`}>Policy</span>
                ) : (
                  <span className={`${styles.badge} ${styles.badgeManual}`}>Manual</span>
                )}
              </td>
              <td className={styles.td}>
                {(() => {
                  const validCond = r.status?.conditions?.find(c => c.type === "Valid");
                  if (validCond?.status === "True") return <span className={styles.valid}>✓</span>;
                  if (validCond?.status === "False") return <span className={styles.invalid}>✕</span>;
                  return <span className={styles.muted}>—</span>;
                })()}
              </td>
              <td className={`${styles.td} ${styles.muted}`}>
                {relativeTime(r.metadata.creationTimestamp)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Endpoint({ kind, name }: { kind: string; name: string }) {
  return (
    <div className={styles.endpoint}>
      <span className={styles.kind}>{kind}</span>
      <span className={styles.name}>{name}</span>
    </div>
  );
}

function sortValue(r: ResourceRelationship, key: SortKey): string | number {
  switch (key) {
    case "type":
      return r.spec.relationshipType.name;
    case "from":
      return `${r.spec.subject.kind}/${r.spec.subject.name}`;
    case "to":
      return `${r.spec.object.kind}/${r.spec.object.name}`;
    case "source":
      return r.spec.source.type;
    case "valid": {
      const vc = r.status?.conditions?.find(c => c.type === "Valid");
      return vc?.status === "True" ? 1 : vc?.status === "False" ? 0 : -1;
    }
    case "created":
      return r.metadata.creationTimestamp
        ? new Date(r.metadata.creationTimestamp).getTime()
        : 0;
  }
}

function relativeTime(ts?: string): string {
  if (!ts) return "—";
  const then = new Date(ts).getTime();
  const diff = Date.now() - then;
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  if (d < 30) return `${d}d ago`;
  const mo = Math.floor(d / 30);
  if (mo < 12) return `${mo}mo ago`;
  return `${Math.floor(mo / 12)}y ago`;
}
