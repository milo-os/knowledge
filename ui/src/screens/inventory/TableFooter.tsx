import { useSearchParams } from "react-router-dom";
import styles from "./TableFooter.module.css";

interface Props {
  shown: number;
  remaining?: number;
  continueToken?: string;
}

export default function TableFooter({ shown, remaining, continueToken }: Props) {
  const [params, setParams] = useSearchParams();
  const onFirstPage = !params.get("page");

  const goPrev = () => {
    const next = new URLSearchParams(params);
    next.delete("page");
    setParams(next, { replace: true });
  };

  const goNext = () => {
    if (!continueToken) return;
    const next = new URLSearchParams(params);
    next.set("page", continueToken);
    setParams(next, { replace: true });
  };

  const total = shown + (remaining ?? 0);
  const more = remaining && remaining > 0 ? "+" : "";

  return (
    <div className={styles.root}>
      <span className={styles.count}>
        Showing 1–{shown} of {total}
        {more}
      </span>
      <div className={styles.pager}>
        <button className={styles.btn} onClick={goPrev} disabled={onFirstPage}>
          Prev
        </button>
        <button className={styles.btn} onClick={goNext} disabled={!continueToken}>
          Next
        </button>
      </div>
    </div>
  );
}
