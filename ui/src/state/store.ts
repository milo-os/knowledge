import { create } from "zustand";
import type { ControlPlaneContextRef } from "../api/resources/resourceRelationship";
import type { GraphQuerySpec } from "../api/resources/graphQuery";

export type ToastKind = "info" | "success" | "error";

export interface Toast {
  id: string;
  kind: ToastKind;
  title: string;
  description?: string;
}

export type LayoutMode = "full" | "collapsed-sidebar";

interface AppState {
  currentContext: ControlPlaneContextRef | null;
  lastQuerySpec: GraphQuerySpec | null;
  showContextBands: boolean;
  layout: LayoutMode;
  toasts: Toast[];
  setCurrentContext: (ctx: ControlPlaneContextRef | null) => void;
  setLastQuerySpec: (spec: GraphQuerySpec | null) => void;
  setShowContextBands: (v: boolean) => void;
  setLayout: (m: LayoutMode) => void;
  addToast: (t: Omit<Toast, "id">) => string;
  removeToast: (id: string) => void;
}

export const useAppStore = create<AppState>((set) => ({
  currentContext: null,
  lastQuerySpec: null,
  showContextBands: true,
  layout: "full",
  toasts: [],
  setCurrentContext: (currentContext) => set({ currentContext }),
  setLastQuerySpec: (lastQuerySpec) => set({ lastQuerySpec }),
  setShowContextBands: (showContextBands) => set({ showContextBands }),
  setLayout: (layout) => set({ layout }),
  addToast: (t) => {
    const id =
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `t-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    set((s) => ({ toasts: [...s.toasts, { ...t, id }] }));
    return id;
  },
  removeToast: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));
