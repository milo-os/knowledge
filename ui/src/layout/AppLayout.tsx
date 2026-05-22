import { NavLink, Outlet } from "react-router-dom";
import styles from "./AppLayout.module.css";

const tabs = [
  { to: "/graph", label: "Graph Explorer" },
  { to: "/relationships", label: "Relationships" },
  { to: "/catalog", label: "Types & Policies" },
] as const;

export default function AppLayout() {
  return (
    <div className={styles.root}>
      <header className={styles.topbar}>
        <span className={styles.brand}>Knowledge</span>
        <nav className={styles.tabs}>
          {tabs.map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              className={({ isActive }) =>
                `${styles.tab} ${isActive ? styles.tabActive : ""}`
              }
            >
              {t.label}
            </NavLink>
          ))}
        </nav>
      </header>
      <div className={styles.body}>
        <main className={styles.main}>
          <Outlet />
        </main>
      </div>
    </div>
  );
}
