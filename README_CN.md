# AWS NLB Controller

[![Build](https://github.com/dev-ops-tools/aws-nlb-controller/actions/workflows/build.yml/badge.svg)](https://github.com/dev-ops-tools/aws-nlb-controller/actions)
[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)
[![Docker](https://img.shields.io/badge/ghcr.io-latest-2496ED?logo=docker)](https://github.com/dev-ops-tools/aws-nlb-controller/pkgs/container/aws-nlb-controller)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[English](README.md) | [测试用例](test/README.md)

集群级共享 NLB —— 一个 NLB，多个 Service，零额外 AWS 成本。使用 `nlb.k8s.aws/*` 注解，委托官方 `aws-load-balancer-controller` 的 StackDeployer 部署，复用其 ServiceAccount 和 IRSA。

## 使用

```yaml
apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: default
  annotations:
    # --- 必填 ---
    nlb.k8s.aws/enabled: "true"
    nlb.k8s.aws/shared-nlb-name: "payments"   # 组名，集群全局作用域
    # --- 可选 ---
    nlb.k8s.aws/scheme: "internal"             # internal | internet-facing
    nlb.k8s.aws/target-type: "ip"              # ip | instance
    nlb.k8s.aws/ip-address-type: "dualstack"   # ipv4 | dualstack
    nlb.k8s.aws/subnets: "subnet-xxx,subnet-yyy"
    nlb.k8s.aws/tags: "team=platform,env=test"
    nlb.k8s.aws/attributes: "load_balancing.cross_zone.enabled=true"
    nlb.k8s.aws/target-group-attributes: "proxy_protocol_v2.enabled=true"
    nlb.k8s.aws/healthcheck-protocol: "HTTP"
    nlb.k8s.aws/healthcheck-port: "traffic-port"
    nlb.k8s.aws/healthcheck-path: "/readyz"
    nlb.k8s.aws/healthcheck-interval: "15"
    nlb.k8s.aws/healthcheck-timeout: "5"
    nlb.k8s.aws/healthcheck-healthy-threshold: "2"
    nlb.k8s.aws/healthcheck-unhealthy-threshold: "4"
    nlb.k8s.aws/healthcheck-success-codes: "200-299"
spec:
  type: ClusterIP
  selector:
    app: api
  ports:
    - name: http
      port: 80
      targetPort: 8080
      protocol: TCP
```

任意 namespace 中使用相同 `shared-nlb-name` 的 Service 共享同一个 NLB，每个 port 独占一个 listener。同组内显式设置的 `scheme`、`ip-address-type`、`attributes`、`tags` 必须一致，未设置则不参与比较。

| 注解 | 官方对应注解 | 状态 |
|---|---|---|
| `enabled` | - | 已实现 |
| `shared-nlb-name` | - | 已实现 |
| `scheme` | `aws-load-balancer-scheme` | 已实现 |
| `target-type` | `aws-load-balancer-nlb-target-type` | 已实现 |
| `subnets` | `aws-load-balancer-subnets` | 已实现 |
| `ip-address-type` | `aws-load-balancer-ip-address-type` | 已实现 |
| `tags` | `aws-load-balancer-additional-resource-tags` | 已实现 |
| `attributes` | `aws-load-balancer-attributes` | 已实现 |
| `target-group-attributes` | `aws-load-balancer-target-group-attributes` | 已实现 |
| `healthcheck-*` | `aws-load-balancer-healthcheck-*` | 已实现 |
| `ssl-*` | `aws-load-balancer-ssl-*` | 暂不支持 |
| `source-ranges` | `load-balancer-source-ranges` | 暂不支持 |
| `security-groups` | `aws-load-balancer-security-groups` | 暂不支持 |

## 限制

- 同一 shared NLB 下不允许重复 listener port。
- 不支持多个 Service 共享同一个 listener port。
- 仅支持 NLB（不支持 ALB、Ingress、Gateway API）。
- 删除 Service 时：有剩余成员则重建 stack（保留 NLB，移除被删 Service 的资源）；最后一个成员删除时触发全量清理。
- 暂不支持 TLS listener、security groups、source ranges、weighted forwarding。

## Helm 安装

需在同一 namespace 下已部署官方 `aws-load-balancer-controller`，复用其 ServiceAccount 和 IRSA。

```bash
helm install aws-nlb-controller ./helm/aws-nlb-controller \
  --namespace kube-system \
  --set controller.clusterName=<cluster-name> \
  --set controller.aws.region=ap-northeast-1
```

Pod 无法访问 EC2 metadata 时需显式指定 VPC：

```bash
--set controller.aws.vpcID=vpc-xxxxxxxx
```

常用 values：

```yaml
controller:
  clusterName: "my-eks"
  leaderElectionID: "aws-nlb-controller-leader"
  aws:
    region: "ap-northeast-1"
    vpcID: ""
```

## CI/CD

推送 `master` 或 `v*` 标签触发 GitHub Actions 构建，镜像推送至 `ghcr.io/dev-ops-tools/aws-nlb-controller`。见 `.github/workflows/build.yml`。

## 本地开发

```bash
gofmt -w cmd internal
go mod tidy
go test ./...
helm lint ./helm/aws-nlb-controller
```

手动构建推送：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/manager ./cmd/controller
docker build -t ghcr.io/dev-ops-tools/aws-nlb-controller:latest .
docker push ghcr.io/dev-ops-tools/aws-nlb-controller:latest
```
