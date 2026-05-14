package annotations

const (
	SupportImplemented = "implemented"
	SupportPlanned     = "planned"
	SupportUnsupported = "unsupported"
)

type SupportEntry struct {
	Name        string
	AWSName     string
	Status      string
	Description string
}

var NLBAnnotationSupport = []SupportEntry{
	{Name: "nlb.k8s.aws/enabled", AWSName: "", Status: SupportImplemented, Description: "启用本 controller 管理该 Service。"},
	{Name: "nlb.k8s.aws/shared-nlb-name", AWSName: "", Status: SupportImplemented, Description: "集群级 shared NLB 分组名。"},
	{Name: "nlb.k8s.aws/scheme", AWSName: "service.beta.kubernetes.io/aws-load-balancer-scheme", Status: SupportImplemented, Description: "NLB scheme: internal 或 internet-facing。"},
	{Name: "nlb.k8s.aws/target-type", AWSName: "service.beta.kubernetes.io/aws-load-balancer-nlb-target-type", Status: SupportImplemented, Description: "TargetGroup target type: instance 或 ip。"},
	{Name: "nlb.k8s.aws/tags", AWSName: "service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags", Status: SupportImplemented, Description: "附加 AWS tags。"},
	{Name: "nlb.k8s.aws/subnets", AWSName: "service.beta.kubernetes.io/aws-load-balancer-subnets", Status: SupportImplemented, Description: "指定 NLB 子网列表。"},
	{Name: "nlb.k8s.aws/ip-address-type", AWSName: "service.beta.kubernetes.io/aws-load-balancer-ip-address-type", Status: SupportImplemented, Description: "IPv4 / dualstack。"},
	{Name: "nlb.k8s.aws/attributes", AWSName: "service.beta.kubernetes.io/aws-load-balancer-attributes", Status: SupportImplemented, Description: "NLB attributes，例如 deletion protection、cross-zone。"},
	{Name: "nlb.k8s.aws/target-group-attributes", AWSName: "service.beta.kubernetes.io/aws-load-balancer-target-group-attributes", Status: SupportImplemented, Description: "TargetGroup attributes。"},
	{Name: "nlb.k8s.aws/healthcheck-protocol", AWSName: "service.beta.kubernetes.io/aws-load-balancer-healthcheck-protocol", Status: SupportImplemented, Description: "健康检查协议。"},
	{Name: "nlb.k8s.aws/healthcheck-port", AWSName: "service.beta.kubernetes.io/aws-load-balancer-healthcheck-port", Status: SupportImplemented, Description: "健康检查端口。"},
	{Name: "nlb.k8s.aws/healthcheck-path", AWSName: "service.beta.kubernetes.io/aws-load-balancer-healthcheck-path", Status: SupportImplemented, Description: "HTTP/HTTPS 健康检查路径。"},
	{Name: "nlb.k8s.aws/healthcheck-interval", AWSName: "service.beta.kubernetes.io/aws-load-balancer-healthcheck-interval", Status: SupportImplemented, Description: "健康检查间隔。"},
	{Name: "nlb.k8s.aws/healthcheck-timeout", AWSName: "service.beta.kubernetes.io/aws-load-balancer-healthcheck-timeout", Status: SupportImplemented, Description: "健康检查超时。"},
	{Name: "nlb.k8s.aws/healthcheck-healthy-threshold", AWSName: "service.beta.kubernetes.io/aws-load-balancer-healthcheck-healthy-threshold", Status: SupportImplemented, Description: "健康检查 healthy threshold。"},
	{Name: "nlb.k8s.aws/healthcheck-unhealthy-threshold", AWSName: "service.beta.kubernetes.io/aws-load-balancer-healthcheck-unhealthy-threshold", Status: SupportImplemented, Description: "健康检查 unhealthy threshold。"},
	{Name: "nlb.k8s.aws/healthcheck-success-codes", AWSName: "service.beta.kubernetes.io/aws-load-balancer-healthcheck-success-codes", Status: SupportImplemented, Description: "HTTP/GRPC 健康检查成功码。"},
	{Name: "nlb.k8s.aws/ssl-cert", AWSName: "service.beta.kubernetes.io/aws-load-balancer-ssl-cert", Status: SupportPlanned, Description: "TLS listener 证书。"},
	{Name: "nlb.k8s.aws/ssl-ports", AWSName: "service.beta.kubernetes.io/aws-load-balancer-ssl-ports", Status: SupportPlanned, Description: "TLS listener 端口。"},
	{Name: "nlb.k8s.aws/backend-protocol", AWSName: "service.beta.kubernetes.io/aws-load-balancer-backend-protocol", Status: SupportPlanned, Description: "后端协议。"},
	{Name: "nlb.k8s.aws/alpn-policy", AWSName: "service.beta.kubernetes.io/aws-load-balancer-alpn-policy", Status: SupportPlanned, Description: "TLS ALPN policy。"},
	{Name: "nlb.k8s.aws/source-ranges", AWSName: "service.beta.kubernetes.io/load-balancer-source-ranges", Status: SupportPlanned, Description: "入站源 CIDR。"},
	{Name: "nlb.k8s.aws/security-groups", AWSName: "service.beta.kubernetes.io/aws-load-balancer-security-groups", Status: SupportPlanned, Description: "NLB security groups。"},
	{Name: "nlb.k8s.aws/eip-allocations", AWSName: "service.beta.kubernetes.io/aws-load-balancer-eip-allocations", Status: SupportUnsupported, Description: "第一版暂不支持静态 EIP。"},
	{Name: "nlb.k8s.aws/private-ipv4-addresses", AWSName: "service.beta.kubernetes.io/aws-load-balancer-private-ipv4-addresses", Status: SupportUnsupported, Description: "第一版暂不支持静态私网 IPv4。"},
	{Name: "nlb.k8s.aws/ipv6-addresses", AWSName: "service.beta.kubernetes.io/aws-load-balancer-ipv6-addresses", Status: SupportUnsupported, Description: "第一版暂不支持静态 IPv6。"},
	{Name: "nlb.k8s.aws/quic-enabled-ports", AWSName: "service.beta.kubernetes.io/aws-load-balancer-quic-enabled-ports", Status: SupportUnsupported, Description: "第一版暂不支持 QUIC。"},
}
