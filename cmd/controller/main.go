package main

import (
	"os"

	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	zapraw "go.uber.org/zap"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	awslbcaws "sigs.k8s.io/aws-load-balancer-controller/pkg/aws"
	awslbcservices "sigs.k8s.io/aws-load-balancer-controller/pkg/aws/services"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/aws/throttle"
	awslbcconfig "sigs.k8s.io/aws-load-balancer-controller/pkg/config"
	awslbcdeploy "sigs.k8s.io/aws-load-balancer-controller/pkg/deploy"
	elbv2deploy "sigs.k8s.io/aws-load-balancer-controller/pkg/deploy/elbv2"
	awslbck8s "sigs.k8s.io/aws-load-balancer-controller/pkg/k8s"
	awsmetrics "sigs.k8s.io/aws-load-balancer-controller/pkg/metrics/aws"
	lbcmetrics "sigs.k8s.io/aws-load-balancer-controller/pkg/metrics/lbc"
	metricsutil "sigs.k8s.io/aws-load-balancer-controller/pkg/metrics/util"
	"sigs.k8s.io/aws-load-balancer-controller/pkg/networking"
	awslbcruntime "sigs.k8s.io/aws-load-balancer-controller/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	sharedannotations "github.com/luomo/aws-nlb-controller/internal/annotations"
	servicecontroller "github.com/luomo/aws-nlb-controller/internal/controller/service"
	shareddeploy "github.com/luomo/aws-nlb-controller/internal/deploy"
	sharedmodel "github.com/luomo/aws-nlb-controller/internal/model"
)

const controllerName = "shared-nlb-service"

func main() {
	controllerCFG, err := loadConfig()
	if err != nil {
		ctrl.Log.Error(err, "unable to load controller config")
		os.Exit(1)
	}

	ctrl.SetLogger(getLoggerWithLogLevel(controllerCFG.LogLevel))

	scheme := k8sruntime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		ctrl.Log.Error(err, "unable to add Kubernetes scheme")
		os.Exit(1)
	}
	if err := elbv2api.AddToScheme(scheme); err != nil {
		ctrl.Log.Error(err, "unable to add TargetGroupBinding scheme")
		os.Exit(1)
	}

	restCFG, err := awslbcconfig.BuildRestConfig(controllerCFG.RuntimeConfig)
	if err != nil {
		ctrl.Log.Error(err, "unable to build REST config")
		os.Exit(1)
	}
	rtOpts, err := awslbcconfig.BuildRuntimeOptions(controllerCFG.RuntimeConfig, scheme)
	if err != nil {
		ctrl.Log.Error(err, "unable to build runtime options")
		os.Exit(1)
	}
	mgr, err := ctrl.NewManager(restCFG, rtOpts)
	if err != nil {
		ctrl.Log.Error(err, "unable to start manager")
		os.Exit(1)
	}

	finalizerManager := awslbck8s.NewDefaultFinalizerManager(mgr.GetClient(), ctrl.Log)
	cloud, err := buildCloud(controllerCFG)
	if err != nil {
		ctrl.Log.Error(err, "unable to build cloud")
		os.Exit(1)
	}
	deployer := buildOfficialDeployer(controllerCFG, mgr, cloud)
	subnetsResolver := networking.NewDefaultSubnetsResolver(
		networking.NewDefaultAZInfoProvider(cloud.EC2(), ctrl.Log),
		cloud.EC2(),
		cloud.VpcID(),
		controllerCFG.ClusterName,
		controllerCFG.FeatureGates.Enabled(awslbcconfig.SubnetsClusterTagCheck),
		false,
		false,
		ctrl.Log.WithName("subnets-resolver"),
	)
	composer := sharedmodel.NewComposer(controllerCFG.ClusterName, cloud.VpcID(), subnetsResolver)

	if err := servicecontroller.NewReconciler(mgr.GetClient(), sharedannotations.NewParser(), finalizerManager, composer, deployer).SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "unable to set up service controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "manager exited")
		os.Exit(1)
	}
}

func loadConfig() (awslbcconfig.ControllerConfig, error) {
	controllerCFG := awslbcconfig.ControllerConfig{
		AWSConfig: awslbcaws.CloudConfig{
			ThrottleConfig: throttle.NewDefaultServiceOperationsThrottleConfig(),
		},
		FeatureGates: awslbcconfig.NewFeatureGates(),
	}

	fs := pflag.NewFlagSet("", pflag.ExitOnError)
	controllerCFG.BindFlags(fs)
	if err := fs.Parse(os.Args[1:]); err != nil {
		return awslbcconfig.ControllerConfig{}, err
	}
	if err := controllerCFG.Validate(); err != nil {
		return awslbcconfig.ControllerConfig{}, err
	}
	return controllerCFG, nil
}

func buildCloud(controllerCFG awslbcconfig.ControllerConfig) (awslbcservices.Cloud, error) {
	awsMetricsCollector := awsmetrics.NewCollector(metrics.Registry)
	return awslbcaws.NewCloud(controllerCFG.AWSConfig, controllerCFG.ClusterName, awsMetricsCollector, ctrl.Log, nil, awslbcaws.DefaultLbStabilizationTime)
}

func buildOfficialDeployer(controllerCFG awslbcconfig.ControllerConfig, mgr ctrl.Manager, cloud awslbcservices.Cloud) shareddeploy.Deployer {
	sgManager := networking.NewDefaultSecurityGroupManager(cloud.EC2(), ctrl.Log)
	sgReconciler := networking.NewDefaultSecurityGroupReconciler(sgManager, ctrl.Log)
	nodeInfoProvider := networking.NewDefaultNodeInfoProvider(cloud.EC2(), ctrl.Log)
	podENIResolver := networking.NewDefaultPodENIInfoResolver(mgr.GetClient(), cloud.EC2(), nodeInfoProvider, cloud.VpcID(), ctrl.Log)
	nodeENIResolver := networking.NewDefaultNodeENIInfoResolver(nodeInfoProvider, ctrl.Log)
	networkingManager := networking.NewDefaultNetworkingManager(mgr.GetClient(), podENIResolver, nodeENIResolver, sgManager, sgReconciler, cloud.VpcID(), controllerCFG.ClusterName, controllerCFG.ServiceTargetENISGTags, ctrl.Log, controllerCFG.DisableRestrictedSGRules)
	elbv2TaggingManager := elbv2deploy.NewDefaultTaggingManager(cloud.ELBV2(), cloud.VpcID(), controllerCFG.FeatureGates, cloud.RGT(), ctrl.Log)
	reconcileCounters := metricsutil.NewReconcileCounters()
	metricsCollector := lbcmetrics.NewCollector(metrics.Registry, mgr, reconcileCounters, ctrl.Log.WithName("controller_metrics"))
	targetGroupCollector := awsmetrics.NewTargetGroupCollector(metrics.Registry)

	stackDeployer := awslbcdeploy.NewDefaultStackDeployer(
		cloud,
		mgr.GetClient(),
		networkingManager,
		sgManager,
		sgReconciler,
		elbv2TaggingManager,
		controllerCFG,
		sharedannotations.Prefix,
		ctrl.Log.WithName("deploy"),
		metricsCollector,
		controllerName,
		controllerCFG.FeatureGates.Enabled(awslbcconfig.EnhancedDefaultBehavior),
		targetGroupCollector,
		false,
	)
	return shareddeploy.NewOfficialDeployer(stackDeployer, metricsCollector, controllerName, cloud.ELBV2())
}

func getLoggerWithLogLevel(logLevel string) logr.Logger {
	zapLevel := zapraw.NewAtomicLevelAt(zapraw.InfoLevel)
	if logLevel == "debug" {
		zapLevel = zapraw.NewAtomicLevelAt(zapraw.DebugLevel)
	}
	logger := zap.New(zap.UseDevMode(false), zap.Level(zapLevel), zap.StacktraceLevel(zapraw.NewAtomicLevelAt(zapraw.FatalLevel)))
	return awslbcruntime.NewConciseLogger(logger)
}
