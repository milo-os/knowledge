import { useEffect, useState } from "react";
import RelationshipTypesGrid from "./RelationshipTypesGrid";
import PoliciesGrid from "./PoliciesGrid";
import CreateRelationshipTypeModal from "./CreateRelationshipTypeModal";
import styles from "./CatalogPage.module.css";

type Section = "types" | "policies";

function parseHash(): Section {
  const h = window.location.hash.replace(/^#/, "");
  return h === "policies" ? "policies" : "types";
}

export default function CatalogPage() {
  const [section, setSection] = useState<Section>(parseHash);
  const [createOpen, setCreateOpen] = useState(false);

  useEffect(() => {
    const onHash = () => setSection(parseHash());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>Knowledge Graph</h1>
        <button
          type="button"
          className={styles.primaryBtn}
          onClick={() => setCreateOpen(true)}
        >
          Create Type
        </button>
      </header>

      <nav className={styles.subnav}>
        <a
          href="#types"
          className={`${styles.subnavLink} ${section === "types" ? styles.subnavActive : ""}`}
        >
          Relationship Types
        </a>
        <a
          href="#policies"
          className={`${styles.subnavLink} ${section === "policies" ? styles.subnavActive : ""}`}
        >
          Discovery Policies
        </a>
      </nav>

      <div className={styles.body}>
        {section === "types" ? <RelationshipTypesGrid /> : <PoliciesGrid />}
      </div>

      <CreateRelationshipTypeModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
      />
    </div>
  );
}
