package service

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sharedannotations "github.com/luomo/aws-nlb-controller/internal/annotations"
)

func TestGroupBuilderBuildForService(t *testing.T) {
	owner := service("default", "api", "payments", 80)
	peer := service("prod", "worker", "payments", 81)
	otherGroup := service("default", "other", "reports", 82)
	unmanaged := service("default", "plain", "", 83)

	builder := NewGroupBuilder(sharedannotations.NewParser())
	group, managed, err := builder.BuildForService([]*corev1.Service{peer, unmanaged, otherGroup, owner}, owner)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !managed {
		t.Fatal("expected owner service to be managed")
	}
	if group.Name != "payments" {
		t.Fatalf("group name = %q, want payments", group.Name)
	}
	if len(group.Members) != 2 {
		t.Fatalf("member count = %d, want 2", len(group.Members))
	}
	if group.Members[0].Service.Namespace != "default" || group.Members[0].Service.Name != "api" || group.Members[1].Service.Namespace != "prod" || group.Members[1].Service.Name != "worker" {
		t.Fatalf("members = %s/%s, %s/%s; want default/api, prod/worker", group.Members[0].Service.Namespace, group.Members[0].Service.Name, group.Members[1].Service.Namespace, group.Members[1].Service.Name)
	}
}

func TestGroupBuilderIgnoresUnmanagedOwner(t *testing.T) {
	owner := service("default", "plain", "", 80)
	builder := NewGroupBuilder(sharedannotations.NewParser())

	_, managed, err := builder.BuildForService([]*corev1.Service{owner}, owner)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if managed {
		t.Fatal("expected owner service to be ignored")
	}
}

func TestGroupBuilderRejectsDuplicateListenerPort(t *testing.T) {
	owner := service("default", "api", "payments", 80)
	peer := service("default", "worker", "payments", 80)
	builder := NewGroupBuilder(sharedannotations.NewParser())

	_, managed, err := builder.BuildForService([]*corev1.Service{owner, peer}, owner)
	if err == nil || !strings.Contains(err.Error(), "duplicate listener port 80") {
		t.Fatalf("expected duplicate port error, got %v", err)
	}
	if !managed {
		t.Fatal("expected duplicate port service group to be managed")
	}
}

func TestGroupBuilderRejectsConflictingLoadBalancerSettings(t *testing.T) {
	tests := []struct {
		name      string
		configure func(owner *corev1.Service, peer *corev1.Service)
		wantErr   string
	}{
		{
			name: "scheme",
			configure: func(owner *corev1.Service, peer *corev1.Service) {
				owner.Annotations["nlb.k8s.aws/scheme"] = "internal"
				peer.Annotations["nlb.k8s.aws/scheme"] = "internet-facing"
			},
			wantErr: "conflicting scheme",
		},
		{
			name: "tags",
			configure: func(owner *corev1.Service, peer *corev1.Service) {
				owner.Annotations["nlb.k8s.aws/tags"] = "team=platform"
				peer.Annotations["nlb.k8s.aws/tags"] = "team=payments"
			},
			wantErr: "conflicting tags",
		},
		{
			name: "attributes",
			configure: func(owner *corev1.Service, peer *corev1.Service) {
				owner.Annotations["nlb.k8s.aws/attributes"] = "load_balancing.cross_zone.enabled=true"
				peer.Annotations["nlb.k8s.aws/attributes"] = "load_balancing.cross_zone.enabled=false"
			},
			wantErr: "conflicting attributes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := service("default", "api", "payments", 80)
			peer := service("prod", "worker", "payments", 81)
			tt.configure(owner, peer)
			builder := NewGroupBuilder(sharedannotations.NewParser())

			_, managed, err := builder.BuildForService([]*corev1.Service{owner, peer}, owner)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
			if !managed {
				t.Fatal("expected conflicting setting service group to be managed")
			}
		})
	}
}

func TestGroupBuilderAllowsExplicitDefaultLoadBalancerSettings(t *testing.T) {
	owner := service("default", "api", "payments", 80)
	peer := service("prod", "worker", "payments", 81)
	owner.Annotations["nlb.k8s.aws/scheme"] = "internal"
	owner.Annotations["nlb.k8s.aws/ip-address-type"] = "ipv4"
	builder := NewGroupBuilder(sharedannotations.NewParser())

	_, managed, err := builder.BuildForService([]*corev1.Service{owner, peer}, owner)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !managed {
		t.Fatal("expected service group to be managed")
	}
}

func service(namespace string, name string, group string, port int32) *corev1.Service {
	annotations := map[string]string{}
	if group != "" {
		annotations["nlb.k8s.aws/enabled"] = "true"
		annotations["nlb.k8s.aws/shared-nlb-name"] = group
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: port},
			},
		},
	}
}
