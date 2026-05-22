#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
CLUSTER_NAME="knowledge-e2e"
NAMESPACE="knowledge-system"

echo "==> Creating kind cluster: ${CLUSTER_NAME}"
if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
  echo "    Cluster already exists, skipping create."
else
  kind create cluster --config "${SCRIPT_DIR}/kind-config.yaml"
fi

kubectl config use-context "kind-${CLUSTER_NAME}"

echo "==> Creating namespace ${NAMESPACE}"
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

echo "==> Deploying Postgres"
kubectl apply -n "${NAMESPACE}" -f - <<'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: knowledge-postgres
type: Opaque
stringData:
  POSTGRES_USER: knowledge
  POSTGRES_PASSWORD: knowledge
  POSTGRES_DB: knowledge
  dsn: "postgres://knowledge:knowledge@postgres.knowledge-system.svc.cluster.local:5432/knowledge?sslmode=disable"
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
spec:
  selector:
    app: postgres
  ports:
    - port: 5432
      targetPort: 5432
  clusterIP: None
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:16-alpine
          ports:
            - containerPort: 5432
          envFrom:
            - secretRef:
                name: knowledge-postgres
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
          readinessProbe:
            exec:
              command: ["pg_isready", "-U", "knowledge"]
            initialDelaySeconds: 5
            periodSeconds: 5
      volumes:
        - name: data
          emptyDir: {}
EOF

echo "==> Waiting for Postgres"
kubectl rollout status statefulset/postgres -n "${NAMESPACE}" --timeout=180s

echo "==> Building Docker images"
cd "${REPO_ROOT}"
docker build -t knowledge-apiserver:latest -f Dockerfile.apiserver .
docker build -t knowledge-controller-manager:latest -f Dockerfile.controller-manager .

echo "==> Loading images into kind"
kind load docker-image knowledge-apiserver:latest --name "${CLUSTER_NAME}"
kind load docker-image knowledge-controller-manager:latest --name "${CLUSTER_NAME}"

# NOTE: We intentionally do NOT apply config/crd/ here. The aggregated
# APIService claims the entire knowledge.miloapis.com/v1alpha1 group, which
# routes all reads/writes for that group to the knowledge-apiserver. If CRDs
# were also installed they would conflict with (and be shadowed by) the
# aggregated server. The aggregated apiserver is the sole source of truth
# for the knowledge.miloapis.com group.

echo "==> Applying apiserver manifests"
kubectl apply -k "${REPO_ROOT}/config/apiserver/"

echo "==> Applying controller-manager manifests"
kubectl apply -k "${REPO_ROOT}/config/controller-manager/"

echo "==> Waiting for apiserver and controller-manager deployments"
kubectl rollout status deployment/knowledge-apiserver -n "${NAMESPACE}" --timeout=300s
kubectl rollout status deployment/knowledge-controller-manager -n "${NAMESPACE}" --timeout=300s

echo "==> Applying sample data"
kubectl apply -k "${REPO_ROOT}/config/samples/"

echo "Setup complete. Run: chainsaw test ../e2e/chainsaw/"
