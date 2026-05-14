package annotations

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	awslbcannotations "sigs.k8s.io/aws-load-balancer-controller/pkg/annotations"
	sharedconstants "sigs.k8s.io/aws-load-balancer-controller/pkg/shared_constants"
)

const (
	Prefix                   = "nlb.k8s.aws"
	AWSServiceAnnotationBase = "service.beta.kubernetes.io"

	SuffixEnabled                = "enabled"
	SuffixSharedNLBName          = "shared-nlb-name"
	SuffixScheme                 = "scheme"
	SuffixTargetType             = "target-type"
	SuffixSubnets                = "subnets"
	SuffixTags                   = "tags"
	SuffixIPAddressType          = "ip-address-type"
	SuffixLoadBalancerAttributes = "attributes"
	SuffixTargetGroupAttributes  = "target-group-attributes"
	SuffixHCHealthyThreshold     = "healthcheck-healthy-threshold"
	SuffixHCUnhealthyThreshold   = "healthcheck-unhealthy-threshold"
	SuffixHCTimeout              = "healthcheck-timeout"
	SuffixHCInterval             = "healthcheck-interval"
	SuffixHCProtocol             = "healthcheck-protocol"
	SuffixHCPort                 = "healthcheck-port"
	SuffixHCPath                 = "healthcheck-path"
	SuffixHCSuccessCodes         = "healthcheck-success-codes"

	SchemeInternetFacing = "internet-facing"
	SchemeInternal       = "internal"

	TargetTypeInstance = "instance"
	TargetTypeIP       = "ip"

	IPAddressTypeIPv4      = "ipv4"
	IPAddressTypeDualStack = "dualstack"
)

type HealthCheckConfig struct {
	Protocol                string
	Port                    string
	Path                    string
	SuccessCodes            string
	IntervalSeconds         *int32
	TimeoutSeconds          *int32
	HealthyThresholdCount   *int32
	UnhealthyThresholdCount *int32
}

type Config struct {
	Enabled                bool
	SharedNLBName          string
	Scheme                 string
	TargetType             string
	Subnets                []string
	Tags                   map[string]string
	IPAddressType          string
	LoadBalancerAttributes map[string]string
	TargetGroupAttributes  map[string]string
	HealthCheck            HealthCheckConfig
}

type Parser struct {
	parser awslbcannotations.Parser
}

func NewParser() *Parser {
	return &Parser{parser: awslbcannotations.NewSuffixAnnotationParser(Prefix)}
}

func (p *Parser) ParseService(svc *corev1.Service) (Config, bool, error) {
	return p.Parse(svc.Annotations)
}

func (p *Parser) Parse(values map[string]string) (Config, bool, error) {
	var cfg Config
	if values == nil {
		return cfg, false, nil
	}

	exists, err := p.parser.ParseBoolAnnotation(SuffixEnabled, &cfg.Enabled, values)
	if err != nil {
		return Config{}, false, err
	}
	if !exists || !cfg.Enabled {
		return cfg, false, nil
	}

	if !p.parser.ParseStringAnnotation(SuffixSharedNLBName, &cfg.SharedNLBName, values) || strings.TrimSpace(cfg.SharedNLBName) == "" {
		return Config{}, true, fmt.Errorf("missing required annotation %s/%s", Prefix, SuffixSharedNLBName)
	}
	cfg.SharedNLBName = strings.TrimSpace(cfg.SharedNLBName)

	if p.parser.ParseStringAnnotation(SuffixScheme, &cfg.Scheme, values) {
		cfg.Scheme = strings.TrimSpace(cfg.Scheme)
		if cfg.Scheme != SchemeInternetFacing && cfg.Scheme != SchemeInternal {
			return Config{}, true, fmt.Errorf("invalid annotation %s/%s: %s", Prefix, SuffixScheme, cfg.Scheme)
		}
	}

	if p.parser.ParseStringAnnotation(SuffixTargetType, &cfg.TargetType, values) {
		cfg.TargetType = strings.TrimSpace(cfg.TargetType)
		if cfg.TargetType != TargetTypeInstance && cfg.TargetType != TargetTypeIP {
			return Config{}, true, fmt.Errorf("invalid annotation %s/%s: %s", Prefix, SuffixTargetType, cfg.TargetType)
		}
	}

	p.parser.ParseStringSliceAnnotation(SuffixSubnets, &cfg.Subnets, values)

	if _, err := p.parser.ParseStringMapAnnotation(SuffixTags, &cfg.Tags, values); err != nil {
		return Config{}, true, err
	}

	if p.parser.ParseStringAnnotation(SuffixIPAddressType, &cfg.IPAddressType, values) {
		cfg.IPAddressType = strings.TrimSpace(cfg.IPAddressType)
		switch cfg.IPAddressType {
		case IPAddressTypeIPv4, IPAddressTypeDualStack:
		default:
			return Config{}, true, fmt.Errorf("invalid annotation %s/%s: %s", Prefix, SuffixIPAddressType, cfg.IPAddressType)
		}
	}

	if _, err := p.parser.ParseStringMapAnnotation(SuffixLoadBalancerAttributes, &cfg.LoadBalancerAttributes, values); err != nil {
		return Config{}, true, err
	}
	if err := validateLoadBalancerAttributes(cfg.LoadBalancerAttributes); err != nil {
		return Config{}, true, err
	}
	if _, err := p.parser.ParseStringMapAnnotation(SuffixTargetGroupAttributes, &cfg.TargetGroupAttributes, values); err != nil {
		return Config{}, true, err
	}
	if err := validateTargetGroupAttributes(cfg.TargetGroupAttributes); err != nil {
		return Config{}, true, err
	}
	if err := p.parseHealthCheck(values, &cfg.HealthCheck); err != nil {
		return Config{}, true, err
	}

	return cfg, true, nil
}

func validateLoadBalancerAttributes(attrs map[string]string) error {
	dnsRecordClientRoutingPolicy, exists := attrs[sharedconstants.LBAttributeLoadBalancingDnsClientRoutingPolicy]
	if !exists {
		return nil
	}
	switch dnsRecordClientRoutingPolicy {
	case sharedconstants.LBAttributeAvailabilityZoneAffinity,
		sharedconstants.LBAttributePartialAvailabilityZoneAffinity,
		sharedconstants.LBAttributeAnyAvailabilityZone:
		return nil
	default:
		return fmt.Errorf("invalid %s set in annotation %s/%s: got %q expected one of [%q, %q, %q]",
			sharedconstants.LBAttributeLoadBalancingDnsClientRoutingPolicy,
			Prefix,
			SuffixLoadBalancerAttributes,
			dnsRecordClientRoutingPolicy,
			sharedconstants.LBAttributeAnyAvailabilityZone,
			sharedconstants.LBAttributePartialAvailabilityZoneAffinity,
			sharedconstants.LBAttributeAvailabilityZoneAffinity)
	}
}

func validateTargetGroupAttributes(attrs map[string]string) error {
	preserveClientIPEnabled, exists := attrs[sharedconstants.TGAttributePreserveClientIPEnabled]
	if !exists {
		return nil
	}
	if _, err := strconv.ParseBool(preserveClientIPEnabled); err != nil {
		return fmt.Errorf("failed to parse attribute %s=%s: %w", sharedconstants.TGAttributePreserveClientIPEnabled, preserveClientIPEnabled, err)
	}
	return nil
}

func (p *Parser) parseHealthCheck(values map[string]string, cfg *HealthCheckConfig) error {
	if p.parser.ParseStringAnnotation(SuffixHCProtocol, &cfg.Protocol, values) {
		cfg.Protocol = strings.ToUpper(strings.TrimSpace(cfg.Protocol))
		switch cfg.Protocol {
		case "TCP", "HTTP", "HTTPS":
		default:
			return fmt.Errorf("invalid annotation %s/%s: unsupported health check protocol %s", Prefix, SuffixHCProtocol, cfg.Protocol)
		}
	}
	if err := p.parseTrimmedStringAnnotation(SuffixHCPort, &cfg.Port, values); err != nil {
		return err
	}
	if err := p.parseTrimmedStringAnnotation(SuffixHCPath, &cfg.Path, values); err != nil {
		return err
	}
	if err := p.parseTrimmedStringAnnotation(SuffixHCSuccessCodes, &cfg.SuccessCodes, values); err != nil {
		return err
	}
	if err := p.parsePositiveInt32Annotation(SuffixHCInterval, &cfg.IntervalSeconds, values); err != nil {
		return err
	}
	if err := p.parsePositiveInt32Annotation(SuffixHCTimeout, &cfg.TimeoutSeconds, values); err != nil {
		return err
	}
	if err := p.parsePositiveInt32Annotation(SuffixHCHealthyThreshold, &cfg.HealthyThresholdCount, values); err != nil {
		return err
	}
	if err := p.parsePositiveInt32Annotation(SuffixHCUnhealthyThreshold, &cfg.UnhealthyThresholdCount, values); err != nil {
		return err
	}
	return nil
}

func (p *Parser) parseTrimmedStringAnnotation(suffix string, out *string, values map[string]string) error {
	if !p.parser.ParseStringAnnotation(suffix, out, values) {
		return nil
	}
	*out = strings.TrimSpace(*out)
	if *out == "" {
		return fmt.Errorf("invalid annotation %s/%s: value must not be empty", Prefix, suffix)
	}
	return nil
}

func (p *Parser) parsePositiveInt32Annotation(suffix string, out **int32, values map[string]string) error {
	var value int32
	exists, err := p.parser.ParseInt32Annotation(suffix, &value, values)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if value <= 0 {
		return fmt.Errorf("invalid annotation %s/%s: value must be greater than 0", Prefix, suffix)
	}
	*out = &value
	return nil
}

func (c Config) ToAWSServiceAnnotations() map[string]string {
	out := map[string]string{
		awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixLoadBalancerType): "external",
	}

	if c.Scheme != "" {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixScheme)] = c.Scheme
	}
	if c.TargetType != "" {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixTargetType)] = c.TargetType
	}
	if len(c.Subnets) != 0 {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixSubnets)] = strings.Join(c.Subnets, ",")
	}
	if len(c.Tags) != 0 {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixAdditionalTags)] = joinStringMap(c.Tags)
	}
	if c.IPAddressType != "" {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixIPAddressType)] = c.IPAddressType
	}
	if len(c.LoadBalancerAttributes) != 0 {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixLoadBalancerAttributes)] = joinStringMap(c.LoadBalancerAttributes)
	}
	if len(c.TargetGroupAttributes) != 0 {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixTargetGroupAttributes)] = joinStringMap(c.TargetGroupAttributes)
	}
	addHealthCheckAWSAnnotations(out, c.HealthCheck)

	return out
}

func addHealthCheckAWSAnnotations(out map[string]string, cfg HealthCheckConfig) {
	if cfg.Protocol != "" {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixHCProtocol)] = cfg.Protocol
	}
	if cfg.Port != "" {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixHCPort)] = cfg.Port
	}
	if cfg.Path != "" {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixHCPath)] = cfg.Path
	}
	if cfg.SuccessCodes != "" {
		out[awsServiceAnnotationKey(awslbcannotations.SvcLBSuffixHCSuccessCodes)] = cfg.SuccessCodes
	}
	addInt32AWSAnnotation(out, awslbcannotations.SvcLBSuffixHCInterval, cfg.IntervalSeconds)
	addInt32AWSAnnotation(out, awslbcannotations.SvcLBSuffixHCTimeout, cfg.TimeoutSeconds)
	addInt32AWSAnnotation(out, awslbcannotations.SvcLBSuffixHCHealthyThreshold, cfg.HealthyThresholdCount)
	addInt32AWSAnnotation(out, awslbcannotations.SvcLBSuffixHCUnhealthyThreshold, cfg.UnhealthyThresholdCount)
}

func addInt32AWSAnnotation(out map[string]string, suffix string, value *int32) {
	if value != nil {
		out[awsServiceAnnotationKey(suffix)] = strconv.FormatInt(int64(*value), 10)
	}
}

func awsServiceAnnotationKey(suffix string) string {
	return AWSServiceAnnotationBase + "/" + suffix
}

func joinStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(values))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ",")
}
