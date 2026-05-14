# aws-nlb-controller 测试用例

[English](README_EN.md)

## 前置条件

1. 集群中已部署 `aws-load-balancer-controller`（官方包）
2. 集群中已部署 `aws-nlb-controller`，启用 `enableAWSDeploy=true`
3. 集群 VPC 子网已打标签 `kubernetes.io/cluster/<cluster-name>: owned` + `kubernetes.io/role/internal-elb: 1`
4. 用于 internet-facing 测试的子网需额外打 `kubernetes.io/role/elb: 1`

## 快速部署与清理

```bash
# 部署指定场景
kubectl apply -f test/<case-dir>/

# 示例
kubectl apply -f test/01-basic-shared-nlb/

# 清理指定场景
kubectl delete -f test/<case-dir>/

# 示例
kubectl delete -f test/01-basic-shared-nlb/
```

## 测试场景

### 01 — 基础共享 NLB

**目的**：验证两个 Service 共享同一个 internal NLB，不同 port 各自独占 listener。

| 文件 | Service | 端口 | 期望 |
|---|---|---|---|
| `demo-api.yaml` | default/demo-api | 80→8080 | 创建 NLB + listener:80 + TG + TGB |
| `demo-worker.yaml` | default/demo-worker | 81→8081 | 复用 NLB + 新增 listener:81 + TG + TGB |

**验证**：
```bash
kubectl get svc demo-api demo-worker
# 两个 Service 的 nlb.k8s.aws/hostname 注解值应相同

aws elbv2 describe-load-balancers --names k8s-<cluster>-payments
# 应有 1 个 NLB，2 个 listener
```

**清理**：
```bash
kubectl delete svc demo-api demo-worker
# 两个都删后 NLB 应被自动清理
```

---

### 02 — 跨 Namespace 共享

**目的**：验证不同 namespace 下相同 `shared-nlb-name` 的 Service 共享 NLB。

| 文件 | Service | 端口 |
|---|---|---|
| `api-default.yaml` | default/shared-api | 80 |
| `worker-prod.yaml` | prod/shared-worker | 81 |

**部署**：
```bash
kubectl create namespace prod --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f test/02-multi-namespace/
```

**验证**：
```bash
kubectl get svc shared-api -n default
kubectl get svc shared-worker -n prod
# 两个 Service nlb.k8s.aws/hostname 一致
```

---

### 03 — 健康检查配置

**目的**：验证 HTTP/TCP 健康检查参数正确下达至 TargetGroup。

| 文件 | Service | 健康检查 |
|---|---|---|
| `hc-api.yaml` | default/hc-api | HTTP /readyz 200-299, interval=15s, timeout=5s, threshold=2/4 |
| `hc-metrics.yaml` | default/hc-metrics | TCP port 9090, interval=10s, threshold=3 |

**验证**：
```bash
TG_ARN=$(aws elbv2 describe-target-groups --names k8s-<cluster>-default-hc-api-80 --query 'TargetGroups[0].TargetGroupArn' --output text)
aws elbv2 describe-target-groups --target-group-arns $TG_ARN
# HealthCheckProtocol: HTTP, HealthCheckPath: /readyz, Matcher.HttpCode: 200-299
```

---

### 04 — Dualstack + Tags + Attributes

**目的**：验证 internet-facing、IPv6 双栈、自定义 tags、NLB/TG attributes 完整链路。

| 文件 | Service | 端口 |
|---|---|---|
| `ds-api.yaml` | default/ds-api | 443→8443 |
| `ds-admin.yaml` | default/ds-admin | 8443→8443 |

**配置要点**：
- Scheme: `internet-facing`，IP type: `dualstack`
- Tags: `team=platform,env=staging`
- NLB attributes: `cross_zone.enabled=true`
- TG attributes: `proxy_protocol_v2.enabled=true`

**验证**：
```bash
aws elbv2 describe-load-balancers --names k8s-<cluster>-ds-group
# Scheme: internet-facing, IpAddressType: dualstack
aws elbv2 describe-tags --resource-arns <NLB-ARN>
# 应包含 team=platform, env=staging
```

---

### 05 — Instance Target Type

**目的**：验证 `instance` 模式通过 NodePort 注册后端。

| 文件 | Service | 端口 |
|---|---|---|
| `inst-api.yaml` | default/inst-api | 80→8080 |
| `inst-worker.yaml` | default/inst-worker | 9090→9090 |

**验证**：
```bash
TG_ARN=$(aws elbv2 describe-target-groups --names k8s-<cluster>-default-inst-api-80 --query 'TargetGroups[0].TargetGroupArn' --output text)
aws elbv2 describe-target-groups --target-group-arns $TG_ARN
# TargetType: instance
```

---

### 06 — 单 Service Group

**目的**：验证只有一个 Service 时正常工作（后续可扩展）。

| 文件 | Service | 端口 |
|---|---|---|
| `single-svc.yaml` | default/single-svc | 80, 9090（双端口） |

---

### 07 — 显式指定子网

**目的**：验证 `subnets` 注解绕过自动发现。

| 文件 | Service | 子网 |
|---|---|---|
| `subnet-api.yaml` | default/subnet-api | 显式 `subnet-xxxxxxxxx,subnet-yyyyyyyyy` |

> **注意**：部署前需将 `subnet-xxxxxxxxx` 替换为实际子网 ID。

---

### 08 — 独立多 Group

**目的**：验证同一集群中多个独立 shared NLB group 互不干扰。

| 文件 | Group | Scheme |
|---|---|---|
| `group-a-api.yaml` | group-a / default/ga-api | internal |
| `group-a-worker.yaml` | group-a / default/ga-worker | internal |
| `group-b-metrics.yaml` | group-b / default/gb-metrics | internet-facing |

**验证**：
```bash
aws elbv2 describe-load-balancers
# 应看到 2 个 NLB: k8s-<cluster>-group-a 和 k8s-<cluster>-group-b
```

---

## 错误场景测试

以下场景应被 controller 拒绝并报错：

### 重复 port 冲突
```bash
# 两个 Service 都监听 port 80
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
# 期望错误: "duplicate listener port 80"
```

### scheme 冲突
```bash
# 同组内一个设 internal，一个设 internet-facing
# 期望错误: "conflicting scheme"
```

### tags 冲突
```bash
# 同组内两个 Service 显式设置了不同的 tags
# 期望错误: "conflicting tags"
```

---

## 批量执行

```bash
# 一键部署所有正常场景
for dir in 0[1-8]-*/; do
  echo "=== Applying $dir ==="
  kubectl apply -f "$dir"
done

# 一键清理
for dir in 0[1-8]-*/; do
  echo "=== Deleting $dir ==="
  kubectl delete -f "$dir"
done
```
