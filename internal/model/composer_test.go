package model

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	elbv2modelk8s "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"

	sharedannotations "github.com/luomo/aws-nlb-controller/internal/annotations"
)

type fakeSubnetsResolver struct{}

func (fakeSubnetsResolver) ResolveViaDiscovery(_ context.Context, _ ...networking.SubnetsResolveOption) ([]ec2types.Subnet, error) {
	return []ec2types.Subnet{
		{SubnetId: aws.String("subnet-a")},
		{SubnetId: aws.String("subnet-b")},
	}, nil
}

func (fakeSubnetsResolver) ResolveViaNameOrIDSlice(_ context.Context, _ []string, _ ...networking.SubnetsResolveOption) ([]ec2types.Subnet, error) {
	return nil, nil
}

func (fakeSubnetsResolver) ResolveViaSelector(_ context.Context, _ elbv2api.SubnetSelector, _ ...networking.SubnetsResolveOption) ([]ec2types.Subnet, error) {
	return nil, nil
}

func (fakeSubnetsResolver) IsSubnetInLocalZoneOrOutpost(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func newTestComposer() *Composer {
	return NewComposer("demo", "vpc-test", fakeSubnetsResolver{})
}

func TestComposerBuild(t *testing.T) {
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: service("default", "api", 80, 8080),
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					Scheme:        sharedannotations.SchemeInternal,
					TargetType:    sharedannotations.TargetTypeIP,
					Tags:          map[string]string{"team": "platform"},
					IPAddressType: "dualstack",
					LoadBalancerAttributes: map[string]string{
						"load_balancing.cross_zone.enabled": "true",
					},
					TargetGroupAttributes: map[string]string{
						"proxy_protocol_v2.enabled": "true",
					},
				},
			},
			{
				Service: service("default", "worker", 81, 8081),
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					Scheme:        sharedannotations.SchemeInternal,
					TargetType:    sharedannotations.TargetTypeIP,
				},
			},
		},
	}

	stack, lb, err := newTestComposer().Build(context.Background(), group)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if stack.StackID() != (core.StackID{Name: "payments"}) {
		t.Fatalf("stack ID = %#v, want payments", stack.StackID())
	}
	if lb.Spec.Type != elbv2model.LoadBalancerTypeNetwork {
		t.Fatalf("load balancer type = %s, want network", lb.Spec.Type)
	}
	if lb.Spec.Scheme != elbv2model.LoadBalancerSchemeInternal {
		t.Fatalf("load balancer scheme = %s, want internal", lb.Spec.Scheme)
	}
	if lb.Spec.IPAddressType != elbv2model.IPAddressTypeDualStack {
		t.Fatalf("load balancer ip address type = %s, want dualstack", lb.Spec.IPAddressType)
	}
	if len(lb.Spec.LoadBalancerAttributes) != 1 || lb.Spec.LoadBalancerAttributes[0].Key != "load_balancing.cross_zone.enabled" || lb.Spec.LoadBalancerAttributes[0].Value != "true" {
		t.Fatalf("load balancer attributes = %#v, want cross-zone attribute", lb.Spec.LoadBalancerAttributes)
	}

	var lbs []*elbv2model.LoadBalancer
	if err := stack.ListResources(&lbs); err != nil {
		t.Fatalf("failed to list load balancers: %v", err)
	}
	if len(lbs) != 1 {
		t.Fatalf("load balancer count = %d, want 1", len(lbs))
	}

	var listeners []*elbv2model.Listener
	if err := stack.ListResources(&listeners); err != nil {
		t.Fatalf("failed to list listeners: %v", err)
	}
	if len(listeners) != 2 {
		t.Fatalf("listener count = %d, want 2", len(listeners))
	}

	var targetGroups []*elbv2model.TargetGroup
	if err := stack.ListResources(&targetGroups); err != nil {
		t.Fatalf("failed to list target groups: %v", err)
	}
	if len(targetGroups) != 2 {
		t.Fatalf("target group count = %d, want 2", len(targetGroups))
	}
	for _, tg := range targetGroups {
		if tg.Spec.TargetType != elbv2model.TargetTypeIP {
			t.Fatalf("target type = %s, want ip", tg.Spec.TargetType)
		}
		assertDefaultTCPHealthCheck(t, tg.Spec.HealthCheckConfig)
	}
	apiTG := targetGroupByPort(t, targetGroups, 8080)
	if got := targetGroupAttrValue(apiTG.Spec.TargetGroupAttributes, "proxy_protocol_v2.enabled"); got != "true" {
		t.Fatalf("proxy protocol target group attribute = %q, want true", got)
	}

	var bindings []*elbv2modelk8s.TargetGroupBindingResource
	if err := stack.ListResources(&bindings); err != nil {
		t.Fatalf("failed to list target group bindings: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("target group binding count = %d, want 2", len(bindings))
	}
	for _, binding := range bindings {
		if binding.Spec.Template.Namespace != "default" {
			t.Fatalf("binding namespace = %s, want default", binding.Spec.Template.Namespace)
		}
		if binding.Spec.Template.Spec.ServiceRef.Name == "" {
			t.Fatal("expected binding service ref name")
		}
	}
}

func TestComposerBuildsHTTPHealthCheckConfig(t *testing.T) {
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: service("default", "api", 80, 8080),
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					TargetType:    sharedannotations.TargetTypeIP,
					HealthCheck: sharedannotations.HealthCheckConfig{
						Protocol:                "HTTP",
						Port:                    "8081",
						Path:                    "/readyz",
						SuccessCodes:            "200-299",
						IntervalSeconds:         int32Ptr(15),
						TimeoutSeconds:          int32Ptr(5),
						HealthyThresholdCount:   int32Ptr(2),
						UnhealthyThresholdCount: int32Ptr(4),
					},
				},
			},
		},
	}

	stack, _, err := newTestComposer().Build(context.Background(), group)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	tg := onlyTargetGroup(t, stack)
	hc := tg.Spec.HealthCheckConfig
	if hc == nil {
		t.Fatal("expected health check config")
	}
	if hc.Protocol != elbv2model.ProtocolHTTP {
		t.Fatalf("health check protocol = %s, want HTTP", hc.Protocol)
	}
	if hc.Port.String() != "8081" {
		t.Fatalf("health check port = %s, want 8081", hc.Port.String())
	}
	if hc.Path == nil || *hc.Path != "/readyz" {
		t.Fatalf("health check path = %v, want /readyz", hc.Path)
	}
	if hc.Matcher == nil || hc.Matcher.HTTPCode == nil || *hc.Matcher.HTTPCode != "200-299" {
		t.Fatalf("health check matcher = %#v, want 200-299", hc.Matcher)
	}
	assertInt32Ptr(t, "interval", hc.IntervalSeconds, 15)
	assertInt32Ptr(t, "timeout", hc.TimeoutSeconds, 5)
	assertInt32Ptr(t, "healthy threshold", hc.HealthyThresholdCount, 2)
	assertInt32Ptr(t, "unhealthy threshold", hc.UnhealthyThresholdCount, 4)
}

func TestComposerUsesInstanceLocalHealthCheckDefaults(t *testing.T) {
	svc := service("default", "api", 80, 8080)
	svc.Spec.Type = corev1.ServiceTypeLoadBalancer
	svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	svc.Spec.HealthCheckNodePort = 32000
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: svc,
				Config:  sharedannotations.Config{SharedNLBName: "payments"},
			},
		},
	}

	stack, _, err := newTestComposer().Build(context.Background(), group)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	hc := onlyTargetGroup(t, stack).Spec.HealthCheckConfig
	if hc.Protocol != elbv2model.ProtocolHTTP {
		t.Fatalf("health check protocol = %s, want HTTP", hc.Protocol)
	}
	if hc.Port.String() != "32000" {
		t.Fatalf("health check port = %s, want 32000", hc.Port.String())
	}
	if hc.Path == nil || *hc.Path != "/healthz" {
		t.Fatalf("health check path = %v, want /healthz", hc.Path)
	}
	assertInt32Ptr(t, "interval", hc.IntervalSeconds, 10)
	assertInt32Ptr(t, "timeout", hc.TimeoutSeconds, 6)
	assertInt32Ptr(t, "healthy threshold", hc.HealthyThresholdCount, 2)
	assertInt32Ptr(t, "unhealthy threshold", hc.UnhealthyThresholdCount, 2)
}

func TestComposerResolvesNamedHealthCheckPortForInstanceTarget(t *testing.T) {
	svc := service("default", "api", 80, 8080)
	svc.Spec.Ports[0].Name = "http"
	svc.Spec.Ports[0].NodePort = 30080
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: svc,
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					TargetType:    sharedannotations.TargetTypeInstance,
					HealthCheck: sharedannotations.HealthCheckConfig{
						Port: "http",
					},
				},
			},
		},
	}

	stack, _, err := newTestComposer().Build(context.Background(), group)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	hc := onlyTargetGroup(t, stack).Spec.HealthCheckConfig
	if hc.Port.String() != "30080" {
		t.Fatalf("health check port = %s, want 30080", hc.Port.String())
	}
}

func TestComposerResolvesNamedHealthCheckPortForIPTarget(t *testing.T) {
	svc := service("default", "api", 80, 8080)
	svc.Spec.Ports[0].Name = "http"
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: svc,
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					TargetType:    sharedannotations.TargetTypeIP,
					HealthCheck: sharedannotations.HealthCheckConfig{
						Port: "http",
					},
				},
			},
		},
	}

	stack, _, err := newTestComposer().Build(context.Background(), group)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	hc := onlyTargetGroup(t, stack).Spec.HealthCheckConfig
	if hc.Port.String() != "8080" {
		t.Fatalf("health check port = %s, want 8080", hc.Port.String())
	}
}

func TestComposerRejectsNamedHealthCheckPortForIPTargetWithNamedTargetPort(t *testing.T) {
	svc := service("default", "api", 80, 8080)
	svc.Spec.Ports[0].Name = "http"
	svc.Spec.Ports[0].TargetPort = intstr.FromString("web")
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: svc,
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					TargetType:    sharedannotations.TargetTypeIP,
					HealthCheck: sharedannotations.HealthCheckConfig{
						Port: "http",
					},
				},
			},
		},
	}

	_, _, err := newTestComposer().Build(context.Background(), group)
	if err == nil || !strings.Contains(err.Error(), "cannot use named healthCheckPort for IP TargetType") {
		t.Fatalf("expected named health check port error, got %v", err)
	}
}

func TestComposerTCPHealthCheckIgnoresPathAndMatcher(t *testing.T) {
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: service("default", "api", 80, 8080),
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					HealthCheck: sharedannotations.HealthCheckConfig{
						Protocol:     "TCP",
						Path:         "/readyz",
						SuccessCodes: "200-299",
					},
				},
			},
		},
	}

	stack, _, err := newTestComposer().Build(context.Background(), group)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	hc := onlyTargetGroup(t, stack).Spec.HealthCheckConfig
	if hc.Path != nil {
		t.Fatalf("TCP health check path = %v, want nil", hc.Path)
	}
	if hc.Matcher != nil {
		t.Fatalf("TCP health check matcher = %#v, want nil", hc.Matcher)
	}
}

func TestComposerRejectsIPv6ServiceWithoutDualStackLoadBalancer(t *testing.T) {
	svc := service("default", "api", 80, 8080)
	svc.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv6Protocol}
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: svc,
				Config:  sharedannotations.Config{SharedNLBName: "payments"},
			},
		},
	}

	_, _, err := newTestComposer().Build(context.Background(), group)
	if err == nil || !strings.Contains(err.Error(), "unsupported IPv6 configuration") {
		t.Fatalf("expected IPv6 configuration error, got %v", err)
	}
}

func TestComposerUsesInstanceNodePortAsTargetGroupPort(t *testing.T) {
	svc := service("default", "api", 80, 8080)
	svc.Spec.Ports[0].NodePort = 30080
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: svc,
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					TargetType:    sharedannotations.TargetTypeInstance,
				},
			},
		},
	}

	stack, _, err := newTestComposer().Build(context.Background(), group)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	tg := onlyTargetGroup(t, stack)
	if tg.Spec.Port == nil || *tg.Spec.Port != 30080 {
		t.Fatalf("target group port = %v, want 30080", tg.Spec.Port)
	}
}

func TestComposerUsesOneForNamedIPTargetPort(t *testing.T) {
	svc := service("default", "api", 80, 8080)
	svc.Spec.Ports[0].TargetPort = intstr.FromString("web")
	group := Group{
		Name: "payments",
		Members: []GroupMember{
			{
				Service: svc,
				Config: sharedannotations.Config{
					SharedNLBName: "payments",
					TargetType:    sharedannotations.TargetTypeIP,
				},
			},
		},
	}

	stack, _, err := newTestComposer().Build(context.Background(), group)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	tg := onlyTargetGroup(t, stack)
	if tg.Spec.Port == nil || *tg.Spec.Port != 1 {
		t.Fatalf("target group port = %v, want 1", tg.Spec.Port)
	}
}

func TestComposerRejectsEmptyGroup(t *testing.T) {
	_, _, err := newTestComposer().Build(context.Background(), Group{Name: "empty"})
	if err == nil {
		t.Fatal("expected error for empty group")
	}
}

func TestShortenName(t *testing.T) {
	name := shortenName("k8s-demo-default-service-with-a-very-long-name", 32)
	if len(name) > 32 {
		t.Fatalf("name length = %d, want <= 32", len(name))
	}
}

func service(namespace string, name string, port int32, targetPort int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       port,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(targetPort),
				},
			},
		},
	}
}

func onlyTargetGroup(t *testing.T, stack core.Stack) *elbv2model.TargetGroup {
	t.Helper()
	var targetGroups []*elbv2model.TargetGroup
	if err := stack.ListResources(&targetGroups); err != nil {
		t.Fatalf("failed to list target groups: %v", err)
	}
	if len(targetGroups) != 1 {
		t.Fatalf("target group count = %d, want 1", len(targetGroups))
	}
	return targetGroups[0]
}

func targetGroupByPort(t *testing.T, targetGroups []*elbv2model.TargetGroup, port int32) *elbv2model.TargetGroup {
	t.Helper()
	for _, tg := range targetGroups {
		if tg.Spec.Port != nil && *tg.Spec.Port == port {
			return tg
		}
	}
	t.Fatalf("expected target group with port %d", port)
	return nil
}

func targetGroupAttrValue(attrs []elbv2model.TargetGroupAttribute, key string) string {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value
		}
	}
	return ""
}

func assertDefaultTCPHealthCheck(t *testing.T, hc *elbv2model.TargetGroupHealthCheckConfig) {
	t.Helper()
	if hc == nil {
		t.Fatal("expected health check config")
	}
	if hc.Protocol != elbv2model.ProtocolTCP {
		t.Fatalf("health check protocol = %s, want TCP", hc.Protocol)
	}
	if hc.Port == nil || hc.Port.String() != "traffic-port" {
		t.Fatalf("health check port = %v, want traffic-port", hc.Port)
	}
	if hc.Path != nil {
		t.Fatalf("health check path = %v, want nil", hc.Path)
	}
	if hc.Matcher != nil {
		t.Fatalf("health check matcher = %#v, want nil", hc.Matcher)
	}
	assertInt32Ptr(t, "interval", hc.IntervalSeconds, 10)
	assertInt32Ptr(t, "timeout", hc.TimeoutSeconds, 10)
	assertInt32Ptr(t, "healthy threshold", hc.HealthyThresholdCount, 3)
	assertInt32Ptr(t, "unhealthy threshold", hc.UnhealthyThresholdCount, 3)
}

func int32Ptr(value int32) *int32 {
	return &value
}

func assertInt32Ptr(t *testing.T, name string, got *int32, want int32) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %d", name, got, want)
	}
}
