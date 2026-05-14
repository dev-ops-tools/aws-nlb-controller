package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	elbv2modelk8s "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	sharedconstants "sigs.k8s.io/aws-load-balancer-controller/pkg/shared_constants"

	sharedannotations "github.com/luomo/aws-nlb-controller/internal/annotations"
)

const (
	defaultHealthCheckPath               = "/"
	defaultHealthCheckMatcherHTTPCode    = "200-399"
	defaultHealthCheckInterval           = int32(10)
	defaultHealthCheckTimeout            = int32(10)
	defaultHealthCheckHealthyThreshold   = int32(3)
	defaultHealthCheckUnhealthyThreshold = int32(3)

	defaultHealthCheckPathForInstanceModeLocal               = "/healthz"
	defaultHealthCheckTimeoutForInstanceModeLocal            = int32(6)
	defaultHealthCheckHealthyThresholdForInstanceModeLocal   = int32(2)
	defaultHealthCheckUnhealthyThresholdForInstanceModeLocal = int32(2)
)

type Composer struct {
	clusterName     string
	vpcID           string
	subnetsResolver networking.SubnetsResolver
}

func NewComposer(clusterName string, vpcID string, subnetsResolver networking.SubnetsResolver) *Composer {
	return &Composer{clusterName: clusterName, vpcID: vpcID, subnetsResolver: subnetsResolver}
}

func (c *Composer) Build(ctx context.Context, group Group) (core.Stack, *elbv2model.LoadBalancer, error) {
	if len(group.Members) == 0 {
		return nil, nil, fmt.Errorf("shared NLB group %s has no members", group.Name)
	}
	return c.build(ctx, group)
}

func (c *Composer) BuildCleanup(ctx context.Context, group Group) (core.Stack, *elbv2model.LoadBalancer, error) {
	if len(group.Members) == 0 {
		return core.NewDefaultStack(core.StackID{Name: group.Name}), nil, nil
	}
	return c.build(ctx, group)
}

// NLBName returns the AWS NLB resource name for a given group name.
func (c *Composer) NLBName(groupName string) string {
	return c.loadBalancerName(groupName)
}

func (c *Composer) build(ctx context.Context, group Group) (core.Stack, *elbv2model.LoadBalancer, error) {
	lbSpec, err := c.buildLoadBalancerSpec(ctx, group)
	if err != nil {
		return nil, nil, err
	}
	stack := core.NewDefaultStack(core.StackID{Name: group.Name})
	lb := elbv2model.NewLoadBalancer(stack, "LoadBalancer", lbSpec)
	for _, member := range group.Members {
		for _, port := range member.Service.Spec.Ports {
			tgSpec, err := c.buildTargetGroupSpec(member, port, lb.Spec.IPAddressType)
			if err != nil {
				return nil, nil, err
			}
			tg := elbv2model.NewTargetGroup(stack, targetGroupResourceID(member.Service, port), tgSpec)
			elbv2modelk8s.NewTargetGroupBindingResource(stack, tg.ID(), c.buildTargetGroupBindingSpec(member, port, tg))
			elbv2model.NewListener(stack, listenerResourceID(port), elbv2model.ListenerSpec{
				LoadBalancerARN: lb.LoadBalancerARN(),
				Port:            port.Port,
				Protocol:        listenerProtocol(port.Protocol),
				DefaultActions: []elbv2model.Action{
					{
						Type: elbv2model.ActionTypeForward,
						ForwardConfig: &elbv2model.ForwardActionConfig{
							TargetGroups: []elbv2model.TargetGroupTuple{{TargetGroupARN: tg.TargetGroupARN()}},
						},
					},
				},
			})
		}
	}

	return stack, lb, nil
}

func (c *Composer) buildLoadBalancerSpec(ctx context.Context, group Group) (elbv2model.LoadBalancerSpec, error) {
	cfg := group.Members[0].Config
	subnetMappings, err := c.resolveSubnetMappings(ctx, cfg)
	if err != nil {
		return elbv2model.LoadBalancerSpec{}, err
	}
	return elbv2model.LoadBalancerSpec{
		Name:                   c.loadBalancerName(group.Name),
		Type:                   elbv2model.LoadBalancerTypeNetwork,
		Scheme:                 loadBalancerScheme(cfg),
		IPAddressType:          loadBalancerIPAddressType(cfg),
		SubnetMappings:         subnetMappings,
		LoadBalancerAttributes: loadBalancerAttributes(cfg),
		Tags:                   cfg.Tags,
	}, nil
}

func (c *Composer) resolveSubnetMappings(ctx context.Context, cfg sharedannotations.Config) ([]elbv2model.SubnetMapping, error) {
	if len(cfg.Subnets) > 0 {
		return buildSubnetMappings(cfg.Subnets), nil
	}
	subnets, err := c.subnetsResolver.ResolveViaDiscovery(ctx,
		networking.WithSubnetsResolveLBType(elbv2model.LoadBalancerTypeNetwork),
		networking.WithSubnetsResolveLBScheme(loadBalancerScheme(cfg)),
	)
	if err != nil {
		return nil, err
	}
	return buildSubnetMappingsFromEC2(subnets), nil
}

func buildSubnetMappingsFromEC2(subnets []ec2types.Subnet) []elbv2model.SubnetMapping {
	mappings := make([]elbv2model.SubnetMapping, 0, len(subnets))
	for _, sn := range subnets {
		mappings = append(mappings, elbv2model.SubnetMapping{SubnetID: awssdk.ToString(sn.SubnetId)})
	}
	return mappings
}

func buildSubnetMappings(subnetIDs []string) []elbv2model.SubnetMapping {
	if len(subnetIDs) == 0 {
		return nil
	}
	mappings := make([]elbv2model.SubnetMapping, 0, len(subnetIDs))
	for _, id := range subnetIDs {
		id := id
		mappings = append(mappings, elbv2model.SubnetMapping{SubnetID: id})
	}
	return mappings
}

func (c *Composer) buildTargetGroupSpec(member GroupMember, port corev1.ServicePort, lbIPAddressType elbv2model.IPAddressType) (elbv2model.TargetGroupSpec, error) {
	targetPort := buildTargetGroupPort(targetType(member.Config), port)
	healthCheckConfig, err := c.buildTargetGroupHealthCheckConfig(member)
	if err != nil {
		return elbv2model.TargetGroupSpec{}, err
	}
	ipAddressType, err := buildTargetGroupIPAddressType(member.Service, lbIPAddressType)
	if err != nil {
		return elbv2model.TargetGroupSpec{}, err
	}
	return elbv2model.TargetGroupSpec{
		Name:                  c.targetGroupName(member.Service, port),
		TargetType:            targetType(member.Config),
		Port:                  awssdk.Int32(targetPort),
		Protocol:              listenerProtocol(port.Protocol),
		IPAddressType:         ipAddressType,
		HealthCheckConfig:     healthCheckConfig,
		TargetGroupAttributes: targetGroupAttributes(member.Config),
	}, nil
}

func (c *Composer) buildTargetGroupHealthCheckConfig(member GroupMember) (*elbv2model.TargetGroupHealthCheckConfig, error) {
	cfg := member.Config.HealthCheck
	tgType := targetType(member.Config)
	defaults := healthCheckDefaultsFor(member.Service, tgType)

	healthCheckProtocol, err := healthCheckProtocol(defaults.protocol, cfg.Protocol)
	if err != nil {
		return nil, err
	}
	healthCheckPort := defaults.port
	if cfg.Port != "" {
		healthCheckPort = cfg.Port
	}
	healthCheckPortValue, err := buildTargetGroupHealthCheckPort(member.Service, healthCheckPort, tgType)
	if err != nil {
		return nil, err
	}

	healthCheckPath := defaults.path
	if cfg.Path != "" {
		healthCheckPath = cfg.Path
	}
	successCodes := defaults.successCodes
	if cfg.SuccessCodes != "" {
		successCodes = cfg.SuccessCodes
	}
	intervalSeconds := int32Value(defaults.intervalSeconds, cfg.IntervalSeconds)
	timeoutSeconds := int32Value(defaults.timeoutSeconds, cfg.TimeoutSeconds)
	healthyThresholdCount := int32Value(defaults.healthyThresholdCount, cfg.HealthyThresholdCount)
	unhealthyThresholdCount := int32Value(defaults.unhealthyThresholdCount, cfg.UnhealthyThresholdCount)

	var path *string
	var matcher *elbv2model.HealthCheckMatcher
	if healthCheckProtocol != elbv2model.ProtocolTCP {
		path = &healthCheckPath
		matcher = &elbv2model.HealthCheckMatcher{HTTPCode: &successCodes}
	}

	return &elbv2model.TargetGroupHealthCheckConfig{
		Port:                    &healthCheckPortValue,
		Protocol:                healthCheckProtocol,
		Path:                    path,
		Matcher:                 matcher,
		IntervalSeconds:         &intervalSeconds,
		TimeoutSeconds:          &timeoutSeconds,
		HealthyThresholdCount:   &healthyThresholdCount,
		UnhealthyThresholdCount: &unhealthyThresholdCount,
	}, nil
}

func (c *Composer) buildTargetGroupBindingSpec(member GroupMember, port corev1.ServicePort, tg *elbv2model.TargetGroup) elbv2modelk8s.TargetGroupBindingResourceSpec {
	targetType := elbv2api.TargetType(tg.Spec.TargetType)
	ipAddressType := elbv2api.TargetGroupIPAddressType(tg.Spec.IPAddressType)
	protocol := tg.Spec.Protocol
	return elbv2modelk8s.TargetGroupBindingResourceSpec{
		Template: elbv2modelk8s.TargetGroupBindingTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: member.Service.Namespace,
				Name:      tg.Spec.Name,
			},
			Spec: elbv2modelk8s.TargetGroupBindingSpec{
				TargetGroupARN:      tg.TargetGroupARN(),
				TargetType:          &targetType,
				ServiceRef:          elbv2api.ServiceReference{Name: member.Service.Name, Port: intstr.FromInt32(port.Port)},
				IPAddressType:       ipAddressType,
				TargetGroupProtocol: &protocol,
				VpcID:               c.vpcID,
			},
		},
	}
}

type healthCheckDefaults struct {
	protocol                elbv2model.Protocol
	port                    string
	path                    string
	successCodes            string
	intervalSeconds         int32
	timeoutSeconds          int32
	healthyThresholdCount   int32
	unhealthyThresholdCount int32
}

func healthCheckDefaultsFor(svc *corev1.Service, targetType elbv2model.TargetType) healthCheckDefaults {
	if targetType == elbv2model.TargetTypeInstance && svc.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		return healthCheckDefaults{
			protocol:                elbv2model.ProtocolHTTP,
			port:                    strconv.Itoa(int(svc.Spec.HealthCheckNodePort)),
			path:                    defaultHealthCheckPathForInstanceModeLocal,
			successCodes:            defaultHealthCheckMatcherHTTPCode,
			intervalSeconds:         defaultHealthCheckInterval,
			timeoutSeconds:          defaultHealthCheckTimeoutForInstanceModeLocal,
			healthyThresholdCount:   defaultHealthCheckHealthyThresholdForInstanceModeLocal,
			unhealthyThresholdCount: defaultHealthCheckUnhealthyThresholdForInstanceModeLocal,
		}
	}
	return healthCheckDefaults{
		protocol:                elbv2model.ProtocolTCP,
		port:                    sharedconstants.HealthCheckPortTrafficPort,
		path:                    defaultHealthCheckPath,
		successCodes:            defaultHealthCheckMatcherHTTPCode,
		intervalSeconds:         defaultHealthCheckInterval,
		timeoutSeconds:          defaultHealthCheckTimeout,
		healthyThresholdCount:   defaultHealthCheckHealthyThreshold,
		unhealthyThresholdCount: defaultHealthCheckUnhealthyThreshold,
	}
}

func healthCheckProtocol(defaultProtocol elbv2model.Protocol, configuredProtocol string) (elbv2model.Protocol, error) {
	if configuredProtocol == "" {
		return defaultProtocol, nil
	}
	switch strings.ToUpper(configuredProtocol) {
	case string(elbv2model.ProtocolTCP):
		return elbv2model.ProtocolTCP, nil
	case string(elbv2model.ProtocolHTTP):
		return elbv2model.ProtocolHTTP, nil
	case string(elbv2model.ProtocolHTTPS):
		return elbv2model.ProtocolHTTPS, nil
	default:
		return "", fmt.Errorf("unsupported health check protocol %s", configuredProtocol)
	}
}

func buildTargetGroupHealthCheckPort(svc *corev1.Service, rawHealthCheckPort string, targetType elbv2model.TargetType) (intstr.IntOrString, error) {
	if rawHealthCheckPort == sharedconstants.HealthCheckPortTrafficPort {
		return intstr.FromString(rawHealthCheckPort), nil
	}
	healthCheckPort := intstr.Parse(rawHealthCheckPort)
	if healthCheckPort.Type == intstr.Int {
		return healthCheckPort, nil
	}

	svcPort, err := lookupServicePort(svc, healthCheckPort)
	if err != nil {
		return intstr.IntOrString{}, fmt.Errorf("failed to resolve healthCheckPort: %w", err)
	}
	if targetType == elbv2model.TargetTypeInstance {
		return intstr.FromInt(int(svcPort.NodePort)), nil
	}
	if svcPort.TargetPort.Type == intstr.Int {
		return svcPort.TargetPort, nil
	}
	return intstr.IntOrString{}, fmt.Errorf("cannot use named healthCheckPort for IP TargetType when service's targetPort is a named port")
}

func lookupServicePort(svc *corev1.Service, port intstr.IntOrString) (corev1.ServicePort, error) {
	for _, svcPort := range svc.Spec.Ports {
		if port.Type == intstr.Int && svcPort.Port == int32(port.IntValue()) {
			return svcPort, nil
		}
		if port.Type == intstr.String && svcPort.Name == port.StrVal {
			return svcPort, nil
		}
	}
	return corev1.ServicePort{}, fmt.Errorf("service %s/%s does not expose port %s", svc.Namespace, svc.Name, port.String())
}

func int32Value(defaultValue int32, configuredValue *int32) int32 {
	if configuredValue != nil {
		return *configuredValue
	}
	return defaultValue
}

func loadBalancerScheme(cfg sharedannotations.Config) elbv2model.LoadBalancerScheme {
	if cfg.Scheme == sharedannotations.SchemeInternetFacing {
		return elbv2model.LoadBalancerSchemeInternetFacing
	}
	return elbv2model.LoadBalancerSchemeInternal
}

func loadBalancerIPAddressType(cfg sharedannotations.Config) elbv2model.IPAddressType {
	if cfg.IPAddressType == sharedannotations.IPAddressTypeDualStack {
		return elbv2model.IPAddressTypeDualStack
	}
	return elbv2model.IPAddressTypeIPV4
}

func buildTargetGroupIPAddressType(svc *corev1.Service, lbIPAddressType elbv2model.IPAddressType) (elbv2model.TargetGroupIPAddressType, error) {
	for _, ipFamily := range svc.Spec.IPFamilies {
		if ipFamily == corev1.IPv6Protocol {
			if lbIPAddressType != elbv2model.IPAddressTypeDualStack {
				return "", fmt.Errorf("unsupported IPv6 configuration, lb not dual-stack")
			}
			return elbv2model.TargetGroupIPAddressTypeIPv6, nil
		}
	}
	return elbv2model.TargetGroupIPAddressTypeIPv4, nil
}

func targetType(cfg sharedannotations.Config) elbv2model.TargetType {
	if cfg.TargetType == sharedannotations.TargetTypeIP {
		return elbv2model.TargetTypeIP
	}
	return elbv2model.TargetTypeInstance
}

func buildTargetGroupPort(targetType elbv2model.TargetType, svcPort corev1.ServicePort) int32 {
	if targetType == elbv2model.TargetTypeInstance {
		return svcPort.NodePort
	}
	if svcPort.TargetPort.Type == intstr.Int {
		return int32(svcPort.TargetPort.IntValue())
	}
	return 1
}

func loadBalancerAttributes(cfg sharedannotations.Config) []elbv2model.LoadBalancerAttribute {
	attrs := make([]elbv2model.LoadBalancerAttribute, 0, len(cfg.LoadBalancerAttributes))
	for _, key := range sortedKeys(cfg.LoadBalancerAttributes) {
		attrs = append(attrs, elbv2model.LoadBalancerAttribute{Key: key, Value: cfg.LoadBalancerAttributes[key]})
	}
	return attrs
}

func targetGroupAttributes(cfg sharedannotations.Config) []elbv2model.TargetGroupAttribute {
	values := map[string]string{
		sharedconstants.TGAttributeProxyProtocolV2Enabled: strconv.FormatBool(false),
	}
	for key, value := range cfg.TargetGroupAttributes {
		values[key] = value
	}
	attrs := make([]elbv2model.TargetGroupAttribute, 0, len(values))
	for _, key := range sortedKeys(values) {
		attrs = append(attrs, elbv2model.TargetGroupAttribute{Key: key, Value: values[key]})
	}
	return attrs
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func listenerProtocol(protocol corev1.Protocol) elbv2model.Protocol {
	switch protocol {
	case corev1.ProtocolUDP:
		return elbv2model.ProtocolUDP
	case corev1.ProtocolTCP:
		return elbv2model.ProtocolTCP
	default:
		return elbv2model.ProtocolTCP
	}
}

func listenerResourceID(port corev1.ServicePort) string {
	return fmt.Sprintf("listener-%d", port.Port)
}

func targetGroupResourceID(svc *corev1.Service, port corev1.ServicePort) string {
	return fmt.Sprintf("%s-%d", types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}.String(), port.Port)
}

func (c *Composer) loadBalancerName(groupName string) string {
	return shortenName("k8s-"+c.clusterName+"-"+groupName, 32)
}

func (c *Composer) targetGroupName(svc *corev1.Service, port corev1.ServicePort) string {
	return shortenName("k8s-"+c.clusterName+"-"+svc.Namespace+"-"+svc.Name+"-"+fmt.Sprint(port.Port), 32)
}

func shortenName(name string, maxLen int) string {
	normalized := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, name)
	if len(normalized) <= maxLen {
		return normalized
	}
	sum := sha256.Sum256([]byte(normalized))
	suffix := hex.EncodeToString(sum[:])[:8]
	return strings.TrimRight(normalized[:maxLen-9], "-") + "-" + suffix
}
