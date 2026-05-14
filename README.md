# AWS NLB Controller

[![Build](https://github.com/dev-ops-tools/aws-nlb-controller/actions/workflows/build.yml/badge.svg)](https://github.com/dev-ops-tools/aws-nlb-controller/actions)
[![Go Version](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)
[![Docker](https://img.shields.io/badge/ghcr.io-latest-2496ED?logo=docker)](https://github.com/dev-ops-tools/aws-nlb-controller/pkgs/container/aws-nlb-controller)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[中文文档](README_CN.md) | [Test Cases](test/README_EN.md)

Cluster-wide shared NLB for Kubernetes Services. One NLB, multiple Services, zero extra AWS cost.
Uses `nlb.k8s.aws/*` annotations, delegates to the official `aws-load-balancer-controller` StackDeployer.
Works alongside the official controller — reuses its ServiceAccount and IRSA.

## Usage

```yaml
apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: default
  annotations:
    # --- required ---
    nlb.k8s.aws/enabled: "true"
    nlb.k8s.aws/shared-nlb-name: "payments"   # group name, cluster-scoped
    # --- optional ---
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

Services in any namespace with the same `shared-nlb-name` share one NLB — each port becomes a separate listener. Same-group Services must use consistent `scheme`, `ip-address-type`, `attributes`, and `tags` when explicitly set.

| Annotation | Official Equivalent | Status |
|---|---|---|
| `enabled` | - | Implemented |
| `shared-nlb-name` | - | Implemented |
| `scheme` | `aws-load-balancer-scheme` | Implemented |
| `target-type` | `aws-load-balancer-nlb-target-type` | Implemented |
| `subnets` | `aws-load-balancer-subnets` | Implemented |
| `ip-address-type` | `aws-load-balancer-ip-address-type` | Implemented |
| `tags` | `aws-load-balancer-additional-resource-tags` | Implemented |
| `attributes` | `aws-load-balancer-attributes` | Implemented |
| `target-group-attributes` | `aws-load-balancer-target-group-attributes` | Implemented |
| `healthcheck-*` | `aws-load-balancer-healthcheck-*` | Implemented |
| `ssl-*` | `aws-load-balancer-ssl-*` | Unsupported |
| `source-ranges` | `load-balancer-source-ranges` | Unsupported |
| `security-groups` | `aws-load-balancer-security-groups` | Unsupported |

## Limitations

- Duplicate listener ports are rejected within the same group.
- Multiple Services cannot share the same listener port.
- Only NLB is supported (no ALB, Ingress, or Gateway API).
- On Service deletion: remaining members trigger a stack rebuild (keeping the NLB); the last member triggers full cleanup including NLB removal.
- TLS listeners, security groups, source ranges, and weighted forwarding are not yet supported.

## Helm Installation

Requires the official `aws-load-balancer-controller` deployed in the same namespace. Reuses its ServiceAccount and IRSA.

```bash
helm install aws-nlb-controller ./helm/aws-nlb-controller \
  --namespace kube-system \
  --set controller.clusterName=<cluster-name> \
  --set controller.aws.region=ap-northeast-1
```

If metadata service is unavailable, specify the VPC explicitly:

```bash
--set controller.aws.vpcID=vpc-xxxxxxxx
```

Key values:

```yaml
controller:
  clusterName: "my-eks"
  leaderElectionID: "aws-nlb-controller-leader"
  aws:
    region: "ap-northeast-1"
    vpcID: ""
```

## CI/CD

GitHub Actions builds and pushes to `ghcr.io/dev-ops-tools/aws-nlb-controller` on push to `master` or `v*` tags. See `.github/workflows/build.yml`.

## Local Development

```bash
gofmt -w cmd internal
go mod tidy
go test ./...
helm lint ./helm/aws-nlb-controller
```

Build and push manually:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/manager ./cmd/controller
docker build -t ghcr.io/dev-ops-tools/aws-nlb-controller:latest .
docker push ghcr.io/dev-ops-tools/aws-nlb-controller:latest
```
