// Package main is the entrypoint for the k8zner-operator.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap/zapcore"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"github.com/milankappen/k8zner/internal/operator/controller"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// Version is set at build time
	Version = "dev"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(k8znerv1alpha1.AddToScheme(scheme))
}

// loadHCloudToken reads the Hetzner Cloud API token from the file named by
// HCLOUD_TOKEN_FILE (preferred: a mounted Secret volume keeps the token out of
// the pod's environment and `kubectl describe` output), falling back to the
// HCLOUD_TOKEN environment variable. Whitespace is trimmed because secrets
// created with `kubectl create secret --from-file` often carry a trailing
// newline, which would make every Hetzner API call fail with 401.
func loadHCloudToken() (string, error) {
	if path := os.Getenv("HCLOUD_TOKEN_FILE"); path != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- path is operator config, not user input
		if err != nil {
			return "", fmt.Errorf("failed to read HCLOUD_TOKEN_FILE: %w", err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("token file %s is empty", path)
		}
		return token, nil
	}

	token := strings.TrimSpace(os.Getenv("HCLOUD_TOKEN"))
	if token == "" {
		return "", fmt.Errorf("either HCLOUD_TOKEN_FILE or HCLOUD_TOKEN must be set")
	}
	return token, nil
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
		leaderElectionID     string
		logLevel             string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "Enable leader election for controller manager.")
	flag.StringVar(&leaderElectionID, "leader-election-id", "k8zner-operator", "The name of the leader election resource.")
	flag.StringVar(&logLevel, "log-level", "info", "Log verbosity: debug, info, or error.")

	// DEBUG=true is the legacy switch, kept so existing deployments don't
	// silently lose debug output when upgrading.
	if os.Getenv("DEBUG") == "true" {
		logLevel = "debug"
	}

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	switch logLevel {
	case "debug":
		opts.Development = true
		opts.Level = zapcore.DebugLevel
	case "info":
		opts.Level = zapcore.InfoLevel
	case "error":
		opts.Level = zapcore.ErrorLevel
	default:
		setupLog.Error(nil, "invalid --log-level, must be debug, info, or error", "value", logLevel)
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("starting k8zner-operator", "version", Version)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderElectionID,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	hcloudToken, err := loadHCloudToken()
	if err != nil {
		setupLog.Error(err, "unable to load Hetzner Cloud token")
		os.Exit(1)
	}

	// Create the cluster reconciler
	reconciler := controller.NewClusterReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor("k8zner-controller"), //nolint:staticcheck // SA1019: migrating to events.EventRecorder requires full API change
		controller.WithHCloudToken(hcloudToken),
		controller.WithMetrics(true),
		controller.WithMaxConcurrentHeals(1),
	)
	if err = reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "K8znerCluster")
		os.Exit(1)
	}

	// Add health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
