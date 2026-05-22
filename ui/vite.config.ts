import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { readFileSync, existsSync } from "fs";
import { Agent } from "https";
import { load as parseYaml } from "js-yaml";
import { homedir } from "os";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));

interface KubeProxyConfig {
  target: string;
  agent: Agent;
}

function loadKubeProxyConfig(): KubeProxyConfig | null {
  const candidates = [
    process.env["KUBECONFIG"],
    resolve(__dirname, "../.test-infra/kubeconfig"),
    resolve(homedir(), ".kube/config"),
  ].filter(Boolean) as string[];

  let raw: string | undefined;
  let kubeconfigPath: string | undefined;
  for (const p of candidates) {
    if (existsSync(p)) {
      raw = readFileSync(p, "utf8");
      kubeconfigPath = p;
      break;
    }
  }
  if (!raw || !kubeconfigPath) return null;

  const kc = parseYaml(raw) as Record<string, unknown>;
  const currentContextName = kc["current-context"] as string;
  const contexts = kc["contexts"] as Array<{ name: string; context: { cluster: string; user: string } }>;
  const clusters = kc["clusters"] as Array<{ name: string; cluster: Record<string, string> }>;
  const users = kc["users"] as Array<{ name: string; user: Record<string, string> }>;

  const contextEntry = contexts?.find((c) => c.name === currentContextName)?.context;
  if (!contextEntry) return null;

  const cluster = clusters?.find((c) => c.name === contextEntry.cluster)?.cluster;
  const user = users?.find((u) => u.name === contextEntry.user)?.user;
  if (!cluster?.["server"]) return null;

  function resolveCredential(dataKey: string, fileKey: string, obj: Record<string, string> | undefined): Buffer | undefined {
    if (!obj) return undefined;
    if (obj[dataKey]) return Buffer.from(obj[dataKey]!, "base64");
    if (obj[fileKey] && existsSync(obj[fileKey]!)) return readFileSync(obj[fileKey]!);
    return undefined;
  }

  const ca   = resolveCredential("certificate-authority-data", "certificate-authority", cluster);
  const cert = resolveCredential("client-certificate-data",    "client-certificate",    user);
  const key  = resolveCredential("client-key-data",            "client-key",            user);

  const agent = new Agent({ ca, cert, key, rejectUnauthorized: !!ca });

  return { target: cluster["server"]!, agent };
}

const kubeProxy = loadKubeProxyConfig();

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    host: true,
    proxy: kubeProxy
      ? {
          "/apis": {
            target: kubeProxy.target,
            changeOrigin: true,
            secure: false,
            agent: kubeProxy.agent,
            // 0 = no timeout, required for ?watch=true streaming connections
            timeout: 0,
            proxyTimeout: 0,
          },
        }
      : undefined,
  },
});
