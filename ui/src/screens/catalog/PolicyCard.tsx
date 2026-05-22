import type { RelationshipPolicy } from "../../api/resources/relationshipPolicy";
import type { Condition } from "../../api/resources/resourceRelationship";
import styles from "./PolicyCard.module.css";

interface Props {
  item: RelationshipPolicy;
}

type Pill = "ready" | "error" | "pending";

function conditionPill(conds: Condition[] | undefined): Pill {
  if (!conds || conds.length === 0) return "pending";
  if (conds.some((c) => c.status === "False")) return "error";
  if (conds.every((c) => c.status === "True")) return "ready";
  return "pending";
}

const PILL_LABEL: Record<Pill, string> = {
  ready: "Ready",
  error: "Error",
  pending: "Pending",
};

export default function PolicyCard({ item }: Props) {
  const pill = conditionPill(item.status?.conditions);
  const edges = item.status?.discoveredEdgesCount ?? 0;

  return (
    <div className={styles.card}>
      <div className={styles.headerRow}>
        <div className={styles.name}>{item.metadata.name}</div>
        <span className={`${styles.pill} ${styles[pill]}`}>{PILL_LABEL[pill]}</span>
      </div>

      <div className={styles.row}>
        <span className={styles.label}>Type</span>
        <span className={styles.value}>{item.spec.relationshipType.name}</span>
      </div>

      <div className={styles.row}>
        <span className={styles.label}>Subject</span>
        <span className={styles.value}>{item.spec.subject.kind}</span>
      </div>

      <div className={styles.row}>
        <span className={styles.label}>Discovered</span>
        <span className={styles.value}>{edges} edges</span>
      </div>
    </div>
  );
}
