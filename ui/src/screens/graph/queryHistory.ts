import type { GraphQuerySpec } from "../../api/resources/graphQuery";

const KEY = "knowledge-ui:queryHistory";
const MAX_ENTRIES = 10;

export interface QueryHistoryEntry {
  spec: GraphQuerySpec;
  at: string;
}

export function loadQueryHistory(): QueryHistoryEntry[] {
  if (typeof window === "undefined") return [];
  const raw = window.localStorage.getItem(KEY);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function pushQueryHistory(spec: GraphQuerySpec): QueryHistoryEntry[] {
  const current = loadQueryHistory();
  const next: QueryHistoryEntry[] = [
    { spec, at: new Date().toISOString() },
    ...current,
  ].slice(0, MAX_ENTRIES);
  window.localStorage.setItem(KEY, JSON.stringify(next));
  return next;
}
