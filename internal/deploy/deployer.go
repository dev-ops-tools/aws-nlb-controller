package deploy

import (
	"context"

	"sigs.k8s.io/aws-load-balancer-controller/pkg/model/core"
	elbv2model "sigs.k8s.io/aws-load-balancer-controller/pkg/model/elbv2"
)

type Deployer interface {
	Deploy(ctx context.Context, stack core.Stack, lb *elbv2model.LoadBalancer) (string, error)
	CleanupNLB(ctx context.Context, lbName string) error
}
