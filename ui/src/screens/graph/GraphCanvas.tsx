import { useEffect, useMemo, useRef, useState } from "react";
import ForceGraph2D, { type ForceGraphMethods } from "react-force-graph-2d";
import { polygonHull } from "d3-polygon";
import { useNavigate } from "react-router-dom";
import type { GraphEdge, GraphNode } from "./graphModel";
import type { ControlPlaneContextKind } from "../../api/resources/resourceRelationship";
import { useAppStore } from "../../state/store";
import styles from "./GraphCanvas.module.css";

interface Props {
  nodes: GraphNode[];
  links: GraphEdge[];
  selectedNodeId: string | null;
  onNodeClick: (id: string) => void;
  onRegisterFit?: (fn: () => void) => void;
}

const CONTEXT_COLOR_VAR: Record<ControlPlaneContextKind, string> = {
  Platform: "--ctx-platform",
  Organization: "--ctx-organization",
  Project: "--ctx-project",
  User: "--ctx-user",
};

function cssVar(name: string): string {
  if (typeof window === "undefined") return "#000";
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function hexWithAlpha(hex: string, alphaHex: string): string {
  const clean = hex.replace("#", "");
  if (clean.length === 6) return `#${clean}${alphaHex}`;
  return hex;
}

export default function GraphCanvas({
  nodes,
  links,
  selectedNodeId,
  onNodeClick,
  onRegisterFit,
}: Props) {
  const navigate = useNavigate();
  const wrapperRef = useRef<HTMLDivElement>(null);
  const fgRef = useRef<ForceGraphMethods | undefined>(undefined);
  const showContextBands = useAppStore((s) => s.showContextBands);
  const [size, setSize] = useState({ width: 800, height: 600 });
  const [bands, setBands] = useState<
    { kind: ControlPlaneContextKind; points: [number, number][] }[]
  >([]);
  const hasAutoFitted = useRef(false);

  useEffect(() => {
    const el = wrapperRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      setSize({ width: el.clientWidth, height: el.clientHeight });
    });
    ro.observe(el);
    setSize({ width: el.clientWidth, height: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  const data = useMemo(
    () => ({
      nodes: nodes.map((n) => ({ ...n })),
      links: links.map((l) => ({ ...l })),
    }),
    [nodes, links],
  );

  const fitView = () => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        fgRef.current?.zoomToFit(400, 60);
      });
    });
  };

  // Re-arm auto-fit and apply force config whenever data changes.
  useEffect(() => {
    hasAutoFitted.current = false;
    onRegisterFit?.(fitView);
    const fg = fgRef.current;
    if (!fg) return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (fg.d3Force("charge") as any)?.strength(-300);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (fg.d3Force("link") as any)?.distance(90);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);

  const selectedGlow = useMemo(() => cssVar("--node-selected-glow") || "#A78BFA44", []);
  const textColor = useMemo(() => cssVar("--text-primary") || "#F0F4F8", []);
  const pillBg = useMemo(() => cssVar("--pill-bg") || "#1E3A5A", []);
  const borderColor = useMemo(() => cssVar("--border") || "#2A4A6B", []);

  const recomputeBands = () => {
    if (!showContextBands) {
      setBands([]);
      return;
    }
    const fg = fgRef.current;
    if (!fg) return;
    const byKind = new Map<ControlPlaneContextKind, [number, number][]>();
    for (const raw of data.nodes) {
      const n = raw as GraphNode;
      if (n.x === undefined || n.y === undefined) continue;
      const screen = fg.graph2ScreenCoords(n.x, n.y);
      const arr = byKind.get(n.controlPlaneContextKind) ?? [];
      arr.push([screen.x, screen.y]);
      byKind.set(n.controlPlaneContextKind, arr);
    }
    const next: { kind: ControlPlaneContextKind; points: [number, number][] }[] = [];
    for (const [kind, pts] of byKind) {
      if (pts.length < 3) continue;
      const hull = polygonHull(pts);
      if (hull) next.push({ kind, points: hull as [number, number][] });
    }
    setBands(next);
  };

  const handleEngineStop = () => {
    recomputeBands();
    if (!hasAutoFitted.current) {
      hasAutoFitted.current = true;
      fitView();
    }
  };

  return (
    <div ref={wrapperRef} className={styles.wrapper}>
      {nodes.length === 0 ? (
        <div className={styles.empty}>Run a query to see the graph.</div>
      ) : (
        <>
          <div className={styles.canvasHost}>
            <ForceGraph2D
              ref={fgRef as never}
              graphData={data}
              width={size.width}
              height={size.height}
              backgroundColor="rgba(0,0,0,0)"
              nodeRelSize={6}
              cooldownTicks={150}
              onEngineTick={recomputeBands}
              onEngineStop={handleEngineStop}
              onZoom={recomputeBands}
              onNodeClick={(n) => onNodeClick((n as GraphNode).id)}
              onLinkClick={(l) =>
                navigate(
                  `/relationships?type=${encodeURIComponent(
                    (l as GraphEdge).relationshipType,
                  )}`,
                )
              }
              nodeCanvasObject={(rawNode, ctx, globalScale) => {
                const n = rawNode as GraphNode;
                const r = 6;
                const ctxKind = n.controlPlaneContextKind;
                const color = cssVar(CONTEXT_COLOR_VAR[ctxKind]) || "#888";
                if (n.id === selectedNodeId) {
                  ctx.save();
                  ctx.shadowBlur = 20;
                  ctx.shadowColor = selectedGlow;
                  ctx.beginPath();
                  ctx.arc(n.x ?? 0, n.y ?? 0, r + 2, 0, 2 * Math.PI);
                  ctx.fillStyle = color;
                  ctx.fill();
                  ctx.restore();
                } else {
                  ctx.beginPath();
                  ctx.arc(n.x ?? 0, n.y ?? 0, r, 0, 2 * Math.PI);
                  ctx.fillStyle = color;
                  ctx.fill();
                }
                ctx.lineWidth = 1 / globalScale;
                ctx.strokeStyle = borderColor;
                ctx.stroke();
                // Show kind prefix only when zoomed in enough, otherwise just name
                const raw = globalScale >= 1.2 ? `${n.kind}/${n.name}` : n.name;
                const label = raw.length > 22 ? raw.slice(0, 20) + "…" : raw;
                const fontSize = 11 / globalScale;
                if (fontSize < 18) {
                  ctx.font = `${fontSize}px sans-serif`;
                  ctx.textAlign = "center";
                  ctx.textBaseline = "top";
                  ctx.fillStyle = textColor;
                  ctx.fillText(label, n.x ?? 0, (n.y ?? 0) + r + 2);
                }
              }}
              linkCanvasObjectMode={() => "after"}
              linkCanvasObject={(rawLink, ctx, globalScale) => {
                const l = rawLink as unknown as {
                  source: GraphNode;
                  target: GraphNode;
                  relationshipType: string;
                };
                const s = l.source;
                const t = l.target;
                if (
                  s.x === undefined ||
                  s.y === undefined ||
                  t.x === undefined ||
                  t.y === undefined
                )
                  return;
                // Only render edge labels when zoomed in enough
                if (globalScale < 0.8) return;
                const mx = (s.x + t.x) / 2;
                const my = (s.y + t.y) / 2;
                const raw = l.relationshipType;
                const label = raw.length > 28 ? raw.slice(0, 26) + "…" : raw;
                const fontSize = 10 / globalScale;
                ctx.font = `${fontSize}px sans-serif`;
                const padX = 4 / globalScale;
                const padY = 2 / globalScale;
                const textWidth = ctx.measureText(label).width;
                const w = textWidth + padX * 2;
                const h = fontSize + padY * 2;
                ctx.fillStyle = pillBg;
                ctx.strokeStyle = borderColor;
                ctx.lineWidth = 0.5 / globalScale;
                const x = mx - w / 2;
                const y = my - h / 2;
                const r = 3 / globalScale;
                ctx.beginPath();
                ctx.moveTo(x + r, y);
                ctx.lineTo(x + w - r, y);
                ctx.quadraticCurveTo(x + w, y, x + w, y + r);
                ctx.lineTo(x + w, y + h - r);
                ctx.quadraticCurveTo(x + w, y + h, x + w - r, y + h);
                ctx.lineTo(x + r, y + h);
                ctx.quadraticCurveTo(x, y + h, x, y + h - r);
                ctx.lineTo(x, y + r);
                ctx.quadraticCurveTo(x, y, x + r, y);
                ctx.closePath();
                ctx.fill();
                ctx.stroke();
                ctx.fillStyle = textColor;
                ctx.textAlign = "center";
                ctx.textBaseline = "middle";
                ctx.fillText(label, mx, my);
              }}
              linkColor={() => borderColor}
            />
          </div>
          {showContextBands && bands.length > 0 ? (
            <svg
              className={styles.bandsOverlay}
              width={size.width}
              height={size.height}
            >
              {bands.map((band) => {
                const color = cssVar(CONTEXT_COLOR_VAR[band.kind]) || "#888";
                const fill = hexWithAlpha(color, "26");
                const d =
                  "M" +
                  band.points.map((p) => `${p[0]},${p[1]}`).join("L") +
                  "Z";
                return (
                  <path
                    key={band.kind}
                    d={d}
                    fill={fill}
                    stroke={color}
                    strokeOpacity={0.4}
                  />
                );
              })}
            </svg>
          ) : null}
        </>
      )}
    </div>
  );
}
