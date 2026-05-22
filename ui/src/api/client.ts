export interface KubernetesListMeta {
  continue?: string;
  remainingItemCount?: number;
  resourceVersion?: string;
}

export interface KubernetesList<T> {
  apiVersion?: string;
  kind?: string;
  metadata: KubernetesListMeta;
  items: T[];
}

// In the browser, requests go to "" (same origin) and Vite proxies /apis → kubectl proxy.
// In SSR / test / in-cluster contexts VITE_API_BASE_URL provides the explicit base.
const BASE_URL =
  typeof window !== "undefined"
    ? ""
    : ((import.meta.env.VITE_API_BASE_URL as string | undefined) ?? "http://localhost:8001");

const TOKEN_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/token";

let cachedToken: string | null = null;
let tokenResolved = false;

async function readServiceAccountToken(): Promise<string | null> {
  if (tokenResolved) return cachedToken;
  tokenResolved = true;
  if (typeof window !== "undefined") return null;
  try {
    const mod = "node:fs/promises";
    const fs = (await import(/* @vite-ignore */ mod)) as {
      readFile: (p: string, enc: string) => Promise<string>;
    };
    cachedToken = (await fs.readFile(TOKEN_PATH, "utf8")).trim();
  } catch {
    cachedToken = null;
  }
  return cachedToken;
}

export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers);
  if (!headers.has("Accept")) headers.set("Accept", "application/json");
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const token = await readServiceAccountToken();
  if (token && !headers.has("Authorization")) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const url = path.startsWith("http") ? path : `${BASE_URL}${path}`;
  const res = await fetch(url, { ...init, headers });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new ApiError(res.status, res.statusText, body, url);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly statusText: string,
    public readonly body: string,
    public readonly url: string,
  ) {
    super(`${status} ${statusText} (${url}): ${body}`);
    this.name = "ApiError";
  }
}

export const apiClient = {
  baseUrl: BASE_URL,
  get<T>(path: string, init?: RequestInit) {
    return apiFetch<T>(path, { ...init, method: "GET" });
  },
  post<T>(path: string, body: unknown, init?: RequestInit) {
    return apiFetch<T>(path, {
      ...init,
      method: "POST",
      body: JSON.stringify(body),
    });
  },
  del<T>(path: string, init?: RequestInit) {
    return apiFetch<T>(path, { ...init, method: "DELETE" });
  },
};
