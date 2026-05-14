package service

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	awslbck8s "sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sharedannotations "github.com/luomo/aws-nlb-controller/internal/annotations"
	shareddeploy "github.com/luomo/aws-nlb-controller/internal/deploy"
	sharedmodel "github.com/luomo/aws-nlb-controller/internal/model"
)

type Reconciler struct {
	client.Client
	parser           *sharedannotations.Parser
	groupBuilder     *GroupBuilder
	composer         *sharedmodel.Composer
	deployer         shareddeploy.Deployer
	finalizerManager awslbck8s.FinalizerManager
}

func NewReconciler(k8sClient client.Client, parser *sharedannotations.Parser, finalizerManager awslbck8s.FinalizerManager, composer *sharedmodel.Composer, deployer shareddeploy.Deployer) *Reconciler {
	return &Reconciler{
		Client:           k8sClient,
		parser:           parser,
		groupBuilder:     NewGroupBuilder(parser),
		composer:         composer,
		deployer:         deployer,
		finalizerManager: finalizerManager,
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	svc := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !svc.DeletionTimestamp.IsZero() {
		if awslbck8s.HasFinalizer(svc, Finalizer) {
			if err := r.cleanupDeletedService(ctx, svc); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, r.finalizerManager.RemoveFinalizers(ctx, svc, Finalizer)
		}
		return ctrl.Result{}, nil
	}

	services := &corev1.ServiceList{}
	if err := r.List(ctx, services); err != nil {
		return ctrl.Result{}, err
	}

	items := make([]*corev1.Service, 0, len(services.Items))
	for i := range services.Items {
		items = append(items, &services.Items[i])
	}
	group, managed, err := r.groupBuilder.BuildForService(items, svc)
	if err != nil || !managed {
		return ctrl.Result{}, err
	}

	if err := r.finalizerManager.AddFinalizers(ctx, svc, Finalizer); err != nil {
		return ctrl.Result{}, err
	}

	stack, lb, err := r.composer.Build(ctx, group)
	if err != nil {
		return ctrl.Result{}, err
	}
	hostname, err := r.deployer.Deploy(ctx, stack, lb)
	if err != nil || hostname == "" {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.updateGroupStatus(ctx, group, hostname)
}

func (r *Reconciler) cleanupDeletedService(ctx context.Context, svc *corev1.Service) error {
	services := &corev1.ServiceList{}
	if err := r.List(ctx, services); err != nil {
		return err
	}

	items := make([]*corev1.Service, 0, len(services.Items))
	deletedKey := types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}
	for i := range services.Items {
		candidate := &services.Items[i]
		candidateKey := types.NamespacedName{Namespace: candidate.Namespace, Name: candidate.Name}
		if candidateKey == deletedKey {
			continue
		}
		items = append(items, candidate)
	}

	deletedCfg, managed, err := r.parser.ParseService(svc)
	if err != nil || !managed {
		return err
	}
	remaining := sharedmodel.Group{Name: deletedCfg.SharedNLBName}
	for _, item := range items {
		cfg, itemManaged, err := r.parser.ParseService(item)
		if err != nil {
			return err
		}
		if itemManaged && cfg.SharedNLBName == deletedCfg.SharedNLBName {
			remaining.Members = append(remaining.Members, sharedmodel.GroupMember{Service: item, Config: cfg})
		}
	}

	stack, lb, err := r.composer.BuildCleanup(ctx, remaining)
	if err != nil {
		return err
	}
	_, err = r.deployer.Deploy(ctx, stack, lb)
	if err != nil {
		return err
	}
	if len(remaining.Members) == 0 {
		if err := r.deployer.CleanupNLB(ctx, r.composer.NLBName(deletedCfg.SharedNLBName)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) updateGroupStatus(ctx context.Context, group sharedmodel.Group, hostname string) error {
	for _, member := range group.Members {
		svc := member.Service
		if !serviceStatusNeedsUpdate(svc, hostname) {
			continue
		}
		oldSvc := svc.DeepCopy()
		if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			svc.Status.LoadBalancer.Ingress = buildLoadBalancerIngress(hostname, svc)
			if err := r.Status().Patch(ctx, svc, client.MergeFrom(oldSvc)); err != nil {
				return err
			}
		} else {
			if svc.Annotations == nil {
				svc.Annotations = make(map[string]string)
			}
			svc.Annotations[sharedannotations.Prefix+"/hostname"] = hostname
			if err := r.Patch(ctx, svc, client.MergeFrom(oldSvc)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(r)
}
