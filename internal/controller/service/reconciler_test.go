package service

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	awslbck8s "sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	sharedannotations "github.com/luomo/aws-nlb-controller/internal/annotations"
	sharedmodel "github.com/luomo/aws-nlb-controller/internal/model"
)

type testSubnetsResolver struct{}

func (testSubnetsResolver) ResolveViaDiscovery(_ context.Context, _ ...networking.SubnetsResolveOption) ([]ec2types.Subnet, error) {
	return []ec2types.Subnet{{SubnetId: aws.String("subnet-a")}, {SubnetId: aws.String("subnet-b")}}, nil
}
func (testSubnetsResolver) ResolveViaNameOrIDSlice(_ context.Context, _ []string, _ ...networking.SubnetsResolveOption) ([]ec2types.Subnet, error) {
	return nil, nil
}
func (testSubnetsResolver) ResolveViaSelector(_ context.Context, _ elbv2api.SubnetSelector, _ ...networking.SubnetsResolveOption) ([]ec2types.Subnet, error) {
	return nil, nil
}
func (testSubnetsResolver) IsSubnetInLocalZoneOrOutpost(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func TestReconcilerAddsFinalizerForManagedService(t *testing.T) {
	svc := service("default", "api", "payments", 80)
	reconciler, finalizers, deployer := newTestReconciler(t, svc)

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "api"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if finalizers.added != 1 || finalizers.removed != 0 {
		t.Fatalf("finalizer calls added=%d removed=%d, want added=1 removed=0", finalizers.added, finalizers.removed)
	}
	if deployer.calls != 1 {
		t.Fatalf("deploy calls = %d, want 1", deployer.calls)
	}
}

func TestReconcilerIgnoresUnmanagedService(t *testing.T) {
	svc := service("default", "api", "", 80)
	reconciler, finalizers, deployer := newTestReconciler(t, svc)

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "api"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if finalizers.added != 0 || finalizers.removed != 0 {
		t.Fatalf("finalizer calls added=%d removed=%d, want no calls", finalizers.added, finalizers.removed)
	}
	if deployer.calls != 0 {
		t.Fatalf("deploy calls = %d, want 0", deployer.calls)
	}
}

func TestReconcilerRemovesFinalizerForDeletingService(t *testing.T) {
	svc := service("default", "api", "payments", 80)
	svc.Finalizers = []string{Finalizer}
	now := metav1.Now()
	svc.DeletionTimestamp = &now
	reconciler, finalizers, deployer := newTestReconciler(t, svc)

	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "api"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if finalizers.added != 0 || finalizers.removed != 1 {
		t.Fatalf("finalizer calls added=%d removed=%d, want added=0 removed=1", finalizers.added, finalizers.removed)
	}
	if deployer.calls != 1 {
		t.Fatalf("deploy calls = %d, want 1", deployer.calls)
	}
}

func TestBuildLoadBalancerIngress(t *testing.T) {
	svc := service("default", "api", "payments", 80)
	svc.Spec.Ports[0].Protocol = corev1.ProtocolTCP

	ingress := buildLoadBalancerIngress("example.elb.amazonaws.com", svc)
	if len(ingress) != 1 {
		t.Fatalf("ingress count = %d, want 1", len(ingress))
	}
	if ingress[0].Hostname != "example.elb.amazonaws.com" {
		t.Fatalf("hostname = %q, want example.elb.amazonaws.com", ingress[0].Hostname)
	}
	if len(ingress[0].Ports) != 1 || ingress[0].Ports[0].Port != 80 || ingress[0].Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("ports = %#v, want TCP 80", ingress[0].Ports)
	}
}

func TestServiceStatusNeedsUpdate(t *testing.T) {
	svc := service("default", "api", "payments", 80)
	if !serviceStatusNeedsUpdate(svc, "example.elb.amazonaws.com") {
		t.Fatal("expected empty status to need update")
	}

	svc.Status.LoadBalancer.Ingress = buildLoadBalancerIngress("example.elb.amazonaws.com", svc)
	if serviceStatusNeedsUpdate(svc, "example.elb.amazonaws.com") {
		t.Fatal("expected matching status to not need update")
	}
	if !serviceStatusNeedsUpdate(svc, "other.elb.amazonaws.com") {
		t.Fatal("expected different hostname to need update")
	}
}

func newTestReconciler(t *testing.T, objects ...client.Object) (*Reconciler, *recordingFinalizerManager, *recordingDeployer) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add core scheme: %v", err)
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	finalizers := &recordingFinalizerManager{}
	deployer := &recordingDeployer{}
	return NewReconciler(k8sClient, sharedannotations.NewParser(), finalizers, sharedmodel.NewComposer("demo", "vpc-test", testSubnetsResolver{}), deployer), finalizers, deployer
}

type recordingDeployer struct {
	calls    int
	hostname string
}

func (d *recordingDeployer) Deploy(_ context.Context, _ core.Stack, _ *elbv2model.LoadBalancer) (string, error) {
	d.calls++
	return d.hostname, nil
}

func (d *recordingDeployer) CleanupNLB(_ context.Context, _ string) error {
	return nil
}

type recordingFinalizerManager struct {
	added   int
	removed int
}

func (m *recordingFinalizerManager) AddFinalizers(_ context.Context, _ client.Object, finalizers ...string) error {
	for _, finalizer := range finalizers {
		if finalizer == Finalizer {
			m.added++
		}
	}
	return nil
}

func (m *recordingFinalizerManager) RemoveFinalizers(_ context.Context, _ client.Object, finalizers ...string) error {
	for _, finalizer := range finalizers {
		if finalizer == Finalizer {
			m.removed++
		}
	}
	return nil
}

var _ awslbck8s.FinalizerManager = (*recordingFinalizerManager)(nil)
