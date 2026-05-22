import { useAppStore } from "../../state/store";
import styles from "./GraphTopBar.module.css";

export default function GraphTopBar({ onFit }: { onFit?: () => void }) {
  const lastQuerySpec = useAppStore((s) => s.lastQuerySpec);
  const showContextBands = useAppStore((s) => s.showContextBands);
  const setShowContextBands = useAppStore((s) => s.setShowContextBands);

  const root = lastQuerySpec?.root;

  return (
    <div className={styles.root}>
      <div className={styles.breadcrumb}>
        <span className={styles.crumb}>Graph</span>
        {root ? (
          <>
            <span className={styles.crumbSep}>/</span>
            <span className={styles.crumb}>{root.kind}</span>
            <span className={styles.crumbSep}>/</span>
            <span className={styles.crumbActive}>{root.name}</span>
          </>
        ) : null}
      </div>
      <div style={{ display: "flex", gap: 8 }}>
        {onFit && (
          <button className={styles.bandsToggle} onClick={onFit} title="Fit graph to view">
            Fit view
          </button>
        )}
        <button
          className={`${styles.bandsToggle} ${
            showContextBands ? styles.bandsToggleActive : ""
          }`}
          onClick={() => setShowContextBands(!showContextBands)}
        >
          Context bands: {showContextBands ? "on" : "off"}
        </button>
      </div>
    </div>
  );
}
