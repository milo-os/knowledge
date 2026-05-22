import { useAppStore } from "../../state/store";

interface Props {
  serverTruncated: boolean;
  depthTruncated: boolean;
}

export default function TruncationBanner({ serverTruncated, depthTruncated }: Props) {
  const lastQuerySpec = useAppStore((s) => s.lastQuerySpec);
  if (!serverTruncated && !depthTruncated) return null;
  const maxNodes = lastQuerySpec?.traverse.maxNodes ?? 0;
  return (
    <div
      style={{
        background: "var(--bg-elevated)",
        borderBottom: "1px solid var(--border)",
        color: "var(--text-primary)",
        padding: "8px 16px",
        fontSize: 13,
      }}
    >
      Result truncated — showing first {maxNodes} of a larger graph.
    </div>
  );
}
