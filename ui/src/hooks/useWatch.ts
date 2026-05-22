import { useEffect } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { apiClient, type KubernetesList } from "../api/client";

type K8sObject = {
  apiVersion?: string;
  kind?: string;
  metadata: { name: string; namespace?: string; resourceVersion?: string };
};

interface WatchEvent<T> {
  type: "ADDED" | "MODIFIED" | "DELETED" | "BOOKMARK" | "ERROR";
  object: T;
}

interface UseWatchOptions {
  resourcePath: string;
  queryKey: QueryKey;
  enabled?: boolean;
}

export function useWatch<T extends K8sObject>({
  resourcePath,
  queryKey,
  enabled = true,
}: UseWatchOptions): void {
  const qc = useQueryClient();

  useEffect(() => {
    if (!enabled) return;

    const controller = new AbortController();
    let cancelled = false;

    const run = async () => {
      const list = qc.getQueryData<KubernetesList<T>>(queryKey);
      let resourceVersion = list?.metadata.resourceVersion;

      if (!resourceVersion) {
        const fresh = await apiClient.get<KubernetesList<T>>(resourcePath);
        if (cancelled) return;
        qc.setQueryData(queryKey, fresh);
        resourceVersion = fresh.metadata.resourceVersion;
      }

      const sep = resourcePath.includes("?") ? "&" : "?";
      const watchUrl =
        `${resourcePath}${sep}watch=true` +
        (resourceVersion ? `&resourceVersion=${resourceVersion}` : "");

      const res = await fetch(
        watchUrl.startsWith("http") ? watchUrl : `${apiClient.baseUrl}${watchUrl}`,
        { signal: controller.signal, headers: { Accept: "application/json" } },
      );
      if (!res.ok || !res.body) return;

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (!cancelled) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let nl: number;
        while ((nl = buffer.indexOf("\n")) >= 0) {
          const line = buffer.slice(0, nl).trim();
          buffer = buffer.slice(nl + 1);
          if (!line) continue;
          let evt: WatchEvent<T>;
          try {
            evt = JSON.parse(line) as WatchEvent<T>;
          } catch {
            continue;
          }
          applyEvent(qc, queryKey, evt);
        }
      }
    };

    run().catch((err) => {
      if ((err as { name?: string }).name !== "AbortError") {
        console.error("watch error", err);
      }
    });

    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [resourcePath, enabled, qc, queryKey]);
}

function applyEvent<T extends K8sObject>(
  qc: ReturnType<typeof useQueryClient>,
  queryKey: QueryKey,
  evt: WatchEvent<T>,
): void {
  qc.setQueryData<KubernetesList<T>>(queryKey, (prev) => {
    const base: KubernetesList<T> = prev ?? {
      metadata: {},
      items: [],
    };
    const idOf = (o: T) => `${o.metadata.namespace ?? ""}/${o.metadata.name}`;
    const id = idOf(evt.object);
    let items = base.items;
    if (evt.type === "ADDED" || evt.type === "MODIFIED") {
      const idx = items.findIndex((o) => idOf(o) === id);
      items = idx >= 0
        ? items.map((o, i) => (i === idx ? evt.object : o))
        : [...items, evt.object];
    } else if (evt.type === "DELETED") {
      items = items.filter((o) => idOf(o) !== id);
    }
    return {
      ...base,
      items,
      metadata: {
        ...base.metadata,
        resourceVersion:
          evt.object.metadata.resourceVersion ?? base.metadata.resourceVersion,
      },
    };
  });
}
