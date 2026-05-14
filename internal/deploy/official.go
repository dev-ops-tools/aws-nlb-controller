package deploy

import (
	"context"
	"fmt"
	"strings"
	"time"

	elbv2sdk "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	awslbcservices "sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	awslbcdeploy "sigs.k8s.io/aws-load-balancer-controller/pkg/deploy"
	lbcmetrics "sigs.k8s.io/aws-load-balancer-controller/pkg/metrics/lbc"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
)

type OfficialDeployer struct {
	stackDeployer awslbcdeploy.StackDeployer
	metrics       lbcmetrics.MetricCollector
	controller    string
	elbv2         awslbcservices.ELBV2
}

func NewOfficialDeployer(stackDeployer awslbcdeploy.StackDeployer, metrics lbcmetrics.MetricCollector, controller string, elbv2 awslbcservices.ELBV2) *OfficialDeployer {
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &OfficialDeployer{stackDeployer: stackDeployer, metrics: metrics, controller: controller, elbv2: elbv2}
}

func (d *OfficialDeployer) CleanupNLB(ctx context.Context, lbName string) error {
	if d.elbv2 == nil {
		return nil
	}
	lbs, err := d.elbv2.DescribeLoadBalancersAsList(ctx, &elbv2sdk.DescribeLoadBalancersInput{
		Names: []string{lbName},
	})
	if err != nil {
		if strings.Contains(err.Error(), "LoadBalancerNotFound") {
			return nil
		}
		return fmt.Errorf("failed to describe load balancer %s: %w", lbName, err)
	}
	if len(lbs) == 0 {
		return nil
	}
	_, err = d.elbv2.DeleteLoadBalancerWithContext(ctx, &elbv2sdk.DeleteLoadBalancerInput{
		LoadBalancerArn: lbs[0].LoadBalancerArn,
	})
	if err != nil {
		return fmt.Errorf("failed to delete load balancer %s: %w", lbName, err)
	}
	return nil
}

func (d *OfficialDeployer) Deploy(ctx context.Context, stack core.Stack, lb *elbv2model.LoadBalancer) (string, error) {
	if err := d.stackDeployer.Deploy(ctx, stack, d.metrics, d.controller); err != nil {
		return "", err
	}
	if lb == nil {
		return "", nil
	}
	dnsName, err := lb.DNSName().Resolve(ctx)
	if err != nil {
		return "", err
	}
	return strings.ToLower(dnsName), nil
}

type noopMetrics struct{}

func (noopMetrics) ObservePodReadinessGateReady(string, string, time.Duration)      {}
func (noopMetrics) ObserveQUICTargetMissingServerId(string, string)                 {}
func (noopMetrics) ObserveControllerReconcileError(string, string)                  {}
func (noopMetrics) ObserveControllerReconcileLatency(_ string, _ string, fn func()) { fn() }
func (noopMetrics) ObserveWebhookValidationError(string, string)                    {}
func (noopMetrics) ObserveWebhookMutationError(string, string)                      {}
func (noopMetrics) StartCollectTopTalkers(context.Context)                          {}
func (noopMetrics) StartCollectCacheSize(context.Context)                           {}
