package annotations

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestParserParse(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantConfig  Config
		wantManaged bool
		wantErr     string
	}{
		{
			name:        "ignores service without enabled annotation",
			annotations: map[string]string{},
			wantManaged: false,
		},
		{
			name: "ignores disabled service",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled": "false",
			},
			wantManaged: false,
		},
		{
			name: "parses minimal managed service",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":         "true",
				"nlb.k8s.aws/shared-nlb-name": "payments",
			},
			wantConfig: Config{
				Enabled:       true,
				SharedNLBName: "payments",
			},
			wantManaged: true,
		},
		{
			name: "parses supported optional annotations",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":                         "true",
				"nlb.k8s.aws/shared-nlb-name":                 "payments",
				"nlb.k8s.aws/scheme":                          "internal",
				"nlb.k8s.aws/target-type":                     "ip",
				"nlb.k8s.aws/subnets":                         "subnet-a, subnet-b",
				"nlb.k8s.aws/tags":                            "team=platform,env=test",
				"nlb.k8s.aws/ip-address-type":                 "dualstack",
				"nlb.k8s.aws/attributes":                      "load_balancing.cross_zone.enabled=true",
				"nlb.k8s.aws/target-group-attributes":         "proxy_protocol_v2.enabled=true",
				"nlb.k8s.aws/healthcheck-protocol":            "http",
				"nlb.k8s.aws/healthcheck-port":                " health ",
				"nlb.k8s.aws/healthcheck-path":                " /readyz ",
				"nlb.k8s.aws/healthcheck-success-codes":       "200-299",
				"nlb.k8s.aws/healthcheck-interval":            "15",
				"nlb.k8s.aws/healthcheck-timeout":             "5",
				"nlb.k8s.aws/healthcheck-healthy-threshold":   "2",
				"nlb.k8s.aws/healthcheck-unhealthy-threshold": "4",
			},
			wantConfig: Config{
				Enabled:       true,
				SharedNLBName: "payments",
				Scheme:        "internal",
				TargetType:    "ip",
				Subnets:       []string{"subnet-a", "subnet-b"},
				Tags: map[string]string{
					"team": "platform",
					"env":  "test",
				},
				IPAddressType: "dualstack",
				LoadBalancerAttributes: map[string]string{
					"load_balancing.cross_zone.enabled": "true",
				},
				TargetGroupAttributes: map[string]string{
					"proxy_protocol_v2.enabled": "true",
				},
				HealthCheck: HealthCheckConfig{
					Protocol:                "HTTP",
					Port:                    "health",
					Path:                    "/readyz",
					SuccessCodes:            "200-299",
					IntervalSeconds:         int32Ptr(15),
					TimeoutSeconds:          int32Ptr(5),
					HealthyThresholdCount:   int32Ptr(2),
					UnhealthyThresholdCount: int32Ptr(4),
				},
			},
			wantManaged: true,
		},
		{
			name: "requires shared nlb name",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled": "true",
			},
			wantManaged: true,
			wantErr:     "missing required annotation nlb.k8s.aws/shared-nlb-name",
		},
		{
			name: "rejects invalid scheme",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":         "true",
				"nlb.k8s.aws/shared-nlb-name": "payments",
				"nlb.k8s.aws/scheme":          "public",
			},
			wantManaged: true,
			wantErr:     "invalid annotation nlb.k8s.aws/scheme: public",
		},
		{
			name: "rejects invalid target type",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":         "true",
				"nlb.k8s.aws/shared-nlb-name": "payments",
				"nlb.k8s.aws/target-type":     "pod",
			},
			wantManaged: true,
			wantErr:     "invalid annotation nlb.k8s.aws/target-type: pod",
		},
		{
			name: "rejects invalid ip address type",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":         "true",
				"nlb.k8s.aws/shared-nlb-name": "payments",
				"nlb.k8s.aws/ip-address-type": "ipv6",
			},
			wantManaged: true,
			wantErr:     "invalid annotation nlb.k8s.aws/ip-address-type: ipv6",
		},
		{
			name: "rejects invalid health check protocol",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":              "true",
				"nlb.k8s.aws/shared-nlb-name":      "payments",
				"nlb.k8s.aws/healthcheck-protocol": "UDP",
			},
			wantManaged: true,
			wantErr:     "unsupported health check protocol UDP",
		},
		{
			name: "rejects empty health check port",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":          "true",
				"nlb.k8s.aws/shared-nlb-name":  "payments",
				"nlb.k8s.aws/healthcheck-port": " ",
			},
			wantManaged: true,
			wantErr:     "invalid annotation nlb.k8s.aws/healthcheck-port: value must not be empty",
		},
		{
			name: "rejects invalid target group preserve client ip attribute",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":                 "true",
				"nlb.k8s.aws/shared-nlb-name":         "payments",
				"nlb.k8s.aws/target-group-attributes": "preserve_client_ip.enabled=maybe",
			},
			wantManaged: true,
			wantErr:     "failed to parse attribute preserve_client_ip.enabled=maybe",
		},
		{
			name: "rejects invalid dns client routing policy attribute",
			annotations: map[string]string{
				"nlb.k8s.aws/enabled":         "true",
				"nlb.k8s.aws/shared-nlb-name": "payments",
				"nlb.k8s.aws/attributes":      "dns_record.client_routing_policy=zone-sticky",
			},
			wantManaged: true,
			wantErr:     "invalid dns_record.client_routing_policy",
		},
	}

	parser := NewParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotConfig, gotManaged, err := parser.Parse(tt.annotations)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if gotManaged != tt.wantManaged {
				t.Fatalf("managed = %v, want %v", gotManaged, tt.wantManaged)
			}
			if !reflect.DeepEqual(gotConfig, tt.wantConfig) {
				t.Fatalf("config = %#v, want %#v", gotConfig, tt.wantConfig)
			}
		})
	}
}

func TestParserParseService(t *testing.T) {
	parser := NewParser()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"nlb.k8s.aws/enabled":         "true",
				"nlb.k8s.aws/shared-nlb-name": "payments",
			},
		},
	}

	cfg, managed, err := parser.ParseService(svc)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !managed {
		t.Fatal("expected service to be managed")
	}
	if cfg.SharedNLBName != "payments" {
		t.Fatalf("shared NLB name = %q, want payments", cfg.SharedNLBName)
	}
}

func TestConfigToAWSServiceAnnotations(t *testing.T) {
	cfg := Config{
		Scheme:        "internal",
		TargetType:    "ip",
		Subnets:       []string{"subnet-a", "subnet-b"},
		IPAddressType: "dualstack",
		Tags: map[string]string{
			"team": "platform",
			"env":  "test",
		},
		LoadBalancerAttributes: map[string]string{
			"load_balancing.cross_zone.enabled": "true",
		},
		TargetGroupAttributes: map[string]string{
			"proxy_protocol_v2.enabled": "true",
		},
		HealthCheck: HealthCheckConfig{
			Protocol:                "HTTP",
			Port:                    "health",
			Path:                    "/readyz",
			SuccessCodes:            "200-299",
			IntervalSeconds:         int32Ptr(15),
			TimeoutSeconds:          int32Ptr(5),
			HealthyThresholdCount:   int32Ptr(2),
			UnhealthyThresholdCount: int32Ptr(4),
		},
	}

	got := cfg.ToAWSServiceAnnotations()
	want := map[string]string{
		"service.beta.kubernetes.io/aws-load-balancer-type":                            "external",
		"service.beta.kubernetes.io/aws-load-balancer-scheme":                          "internal",
		"service.beta.kubernetes.io/aws-load-balancer-nlb-target-type":                 "ip",
		"service.beta.kubernetes.io/aws-load-balancer-subnets":                         "subnet-a,subnet-b",
		"service.beta.kubernetes.io/aws-load-balancer-additional-resource-tags":        "env=test,team=platform",
		"service.beta.kubernetes.io/aws-load-balancer-ip-address-type":                 "dualstack",
		"service.beta.kubernetes.io/aws-load-balancer-attributes":                      "load_balancing.cross_zone.enabled=true",
		"service.beta.kubernetes.io/aws-load-balancer-target-group-attributes":         "proxy_protocol_v2.enabled=true",
		"service.beta.kubernetes.io/aws-load-balancer-healthcheck-protocol":            "HTTP",
		"service.beta.kubernetes.io/aws-load-balancer-healthcheck-port":                "health",
		"service.beta.kubernetes.io/aws-load-balancer-healthcheck-path":                "/readyz",
		"service.beta.kubernetes.io/aws-load-balancer-healthcheck-success-codes":       "200-299",
		"service.beta.kubernetes.io/aws-load-balancer-healthcheck-interval":            "15",
		"service.beta.kubernetes.io/aws-load-balancer-healthcheck-timeout":             "5",
		"service.beta.kubernetes.io/aws-load-balancer-healthcheck-healthy-threshold":   "2",
		"service.beta.kubernetes.io/aws-load-balancer-healthcheck-unhealthy-threshold": "4",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("annotations = %#v, want %#v", got, want)
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
