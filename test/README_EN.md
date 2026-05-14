# aws-nlb-controller Test Cases

[中文](README.md)

## Prerequisites

1. `aws-load-balancer-controller` (official) deployed in the cluster
2. `aws-nlb-controller` deployed with `enableAWSDeploy=true`
3. Cluster VPC subnets tagged with `kubernetes.io/cluster/<cluster-name>: owned` + `kubernetes.io/role/internal-elb: 1`
4. Internet-facing test subnets additionally need `kubernetes.io/role/elb: 1`

## Quick Deploy & Cleanup

```bash
# Deploy a specific scenario
kubectl apply -f test/<case-dir>/

# Example
kubectl apply -f test/01-basic-shared-nlb/

# Cleanup a specific scenario
kubectl delete -f test/<case-dir>/

# Example
kubectl delete -f test/01-basic-shared-nlb/
```

## Test Scenarios

### 01 — Basic Shared NLB

**Goal**: Verify two Services share one internal NLB with different ports.

| File | Service | Port | Expected |
|---|---|---|---|
| `demo-api.yaml` | default/demo-api | 80→8080 | NLB + listener:80 + TG + TGB |
| `demo-worker.yaml` | default/demo-worker | 81→8081 | Reuses NLB + listener:81 + TG + TGB |

**Verify**:
```bash
kubectl get svc demo-api demo-worker
# Both Services should have the same nlb.k8s.aws/hostname annotation

aws elbv2 describe-load-balancers --names k8s-<cluster>-payments
# Should show 1 NLB, 2 listeners
```

**Cleanup**:
```bash
kubectl delete svc demo-api demo-worker
# NLB should be automatically cleaned up after both Services are deleted
```

---

### 02 — Cross-Namespace Sharing

**Goal**: Verify Services in different namespaces with the same `shared-nlb-name` share one NLB.

| File | Service | Port |
|---|---|---|
| `api-default.yaml` | default/shared-api | 80 |
| `worker-prod.yaml` | prod/shared-worker | 81 |

**Deploy**:
```bash
kubectl create namespace prod --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f test/02-multi-namespace/
```

**Verify**:
```bash
kubectl get svc shared-api -n default
kubectl get svc shared-worker -n prod
# Both should have the same nlb.k8s.aws/hostname value
```

---

### 03 — Health Check Configuration

**Goal**: Verify HTTP/TCP health check parameters are correctly applied to TargetGroups.

| File | Service | Health Check |
|---|---|---|
| `hc-api.yaml` | default/hc-api | HTTP /readyz 200-299, interval=15s, timeout=5s, threshold=2/4 |
| `hc-metrics.yaml` | default/hc-metrics | TCP port 9090, interval=10s, threshold=3 |

**Verify**:
```bash
TG_ARN=$(aws elbv2 describe-target-groups --names k8s-<cluster>-default-hc-api-80 --query 'TargetGroups[0].TargetGroupArn' --output text)
aws elbv2 describe-target-groups --target-group-arns $TG_ARN
# HealthCheckProtocol: HTTP, HealthCheckPath: /readyz, Matcher.HttpCode: 200-299
```

---

### 04 — Dualstack + Tags + Attributes

**Goal**: Verify internet-facing, IPv6 dualstack, custom tags, and NLB/TG attributes.

| File | Service | Port |
|---|---|---|
| `ds-api.yaml` | default/ds-api | 443→8443 |
| `ds-admin.yaml` | default/ds-admin | 8443→8443 |

**Key Config**:
- Scheme: `internet-facing`, IP type: `dualstack`
- Tags: `team=platform,env=staging`
- NLB attributes: `cross_zone.enabled=true`
- TG attributes: `proxy_protocol_v2.enabled=true`

**Verify**:
```bash
aws elbv2 describe-load-balancers --names k8s-<cluster>-ds-group
# Scheme: internet-facing, IpAddressType: dualstack
aws elbv2 describe-tags --resource-arns <NLB-ARN>
# Should include team=platform, env=staging
```

---

### 05 — Instance Target Type

**Goal**: Verify `instance` mode registers backends via NodePort.

| File | Service | Port |
|---|---|---|
| `inst-api.yaml` | default/inst-api | 80→8080 |
| `inst-worker.yaml` | default/inst-worker | 9090→9090 |

**Verify**:
```bash
TG_ARN=$(aws elbv2 describe-target-groups --names k8s-<cluster>-default-inst-api-80 --query 'TargetGroups[0].TargetGroupArn' --output text)
aws elbv2 describe-target-groups --target-group-arns $TG_ARN
# TargetType: instance
```

---

### 06 — Single Service Group

**Goal**: Verify a group with a single Service works correctly (can be extended later).

| File | Service | Ports |
|---|---|---|
| `single-svc.yaml` | default/single-svc | 80, 9090 (dual port) |

---

### 07 — Explicit Subnets

**Goal**: Verify the `subnets` annotation bypasses auto-discovery.

| File | Service | Subnets |
|---|---|---|
| `subnet-api.yaml` | default/subnet-api | Explicit `subnet-xxxxxxxxx,subnet-yyyyyyyyy` |

> **Note**: Replace `subnet-xxxxxxxxx` with real subnet IDs before deploying.

---

### 08 — Multiple Independent Groups

**Goal**: Verify multiple independent shared NLB groups coexist without interference.

| File | Group | Scheme |
|---|---|---|
| `group-a-api.yaml` | group-a / default/ga-api | internal |
| `group-a-worker.yaml` | group-a / default/ga-worker | internal |
| `group-b-metrics.yaml` | group-b / default/gb-metrics | internet-facing |

**Verify**:
```bash
aws elbv2 describe-load-balancers
# Should see 2 NLBs: k8s-<cluster>-group-a and k8s-<cluster>-group-b
```

---

## Error Scenario Tests

The following should be rejected by the controller:

### Duplicate Port Conflict
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: conflict-A
  annotations:
    nlb.k8s.aws/enabled: "true"
    nlb.k8s.aws/shared-nlb-name: "conflict-group"
spec:
  type: ClusterIP
  selector: {app: a}
  ports: [{port: 80, targetPort: 8080, protocol: TCP}]
---
apiVersion: v1
kind: Service
metadata:
  name: conflict-B
  annotations:
    nlb.k8s.aws/enabled: "true"
    nlb.k8s.aws/shared-nlb-name: "conflict-group"
spec:
  type: ClusterIP
  selector: {app: b}
  ports: [{port: 80, targetPort: 8081, protocol: TCP}]
EOF
# Expected error: "duplicate listener port 80"
```

### Scheme Conflict
```bash
# One Service sets internal, another sets internet-facing within the same group
# Expected error: "conflicting scheme"
```

### Tags Conflict
```bash
# Two Services explicitly set different tags within the same group
# Expected error: "conflicting tags"
```

---

## Batch Execution

```bash
# Deploy all normal scenarios at once
for dir in 0[1-8]-*/; do
  echo "=== Applying $dir ==="
  kubectl apply -f "$dir"
done

# Cleanup all at once
for dir in 0[1-8]-*/; do
  echo "=== Deleting $dir ==="
  kubectl delete -f "$dir"
done
```
