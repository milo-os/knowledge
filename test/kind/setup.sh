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
kubectl apply -n "${NAMESPACE}" -f "${SCRIPT_DIR}/postgres.yaml"

echo "==> Waiting for Postgres"
kubectl rollout status statefulset/postgres -n "${NAMESPACE}" --timeout=180s

echo "==> Building Docker image"
cd "${REPO_ROOT}"
docker build -t knowledge:dev .

echo "==> Loading image into kind"
kind load docker-image knowledge:dev --name "${CLUSTER_NAME}"

# NOTE: We intentionally do NOT apply config/crd/ here. The aggregated
# APIService claims the entire knowledge.miloapis.com/v1alpha1 group, which
# routes all reads/writes for that group to the knowledge-apiserver. If CRDs
# were also installed they would conflict with (and be shadowed by) the
# aggregated server. The aggregated apiserver is the sole source of truth
# for the knowledge.miloapis.com group.

echo "==> Applying manifests (e2e overlay)"
kubectl apply -k "${REPO_ROOT}/config/overlays/e2e/"

echo "==> Waiting for apiserver and controller-manager deployments"
kubectl rollout status deployment/knowledge-apiserver -n "${NAMESPACE}" --timeout=300s
kubectl rollout status deployment/knowledge-controller-manager -n "${NAMESPACE}" --timeout=300s

echo "==> Applying sample data"
kubectl apply -k "${REPO_ROOT}/config/samples/"

echo "Setup complete. Run: chainsaw test test/e2e/chainsaw/"
