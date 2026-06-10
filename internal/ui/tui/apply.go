package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RunApplyTUI wraps the bootstrap flow with a Bubble Tea TUI.
// bootstrapFn runs the CLI bootstrap, sending phase updates on the channel.
// After bootstrap completes, if wait is true, it switches to CRD polling mode.
func RunApplyTUI(
	ctx context.Context,
	bootstrapFn func(ch chan<- BootstrapPhaseMsg) error,
	clusterName, region string,
	kubeconfig []byte,
	wait bool,
) error {
	m := NewApplyModel(clusterName, region)

	p := tea.NewProgram(m, tea.WithAltScreen())

	// Run bootstrap in background goroutine
	go func() {
		ch := make(chan BootstrapPhaseMsg, 10)
		go func() {
			defer close(ch)
			if err := bootstrapFn(ch); err != nil {
				p.Send(ErrMsg{Err: err})
			}
		}()

		for msg := range ch {
			p.Send(msg)
		}

		// Bootstrap done, start CRD polling if wait requested
		if wait && len(kubeconfig) > 0 {
			pollCRDStatus(ctx, p, clusterName, kubeconfig)
		} else {
			p.Send(DoneMsg{})
		}
	}()

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	fm := finalModel.(Model)
	if fm.Err != nil {
		return fm.Err
	}
	return nil
}

// pollCRDStatus polls the K8znerCluster CRD and sends status updates to the TUI.
func pollCRDStatus(ctx context.Context, p *tea.Program, clusterName string, kubeconfig []byte) {
	kubecfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		p.Send(ErrMsg{Err: fmt.Errorf("failed to parse kubeconfig: %w", err)})
		return
	}

	scheme := k8znerv1alpha1.Scheme
	k8sClient, err := client.New(kubecfg, client.Options{Scheme: scheme})
	if err != nil {
		p.Send(ErrMsg{Err: fmt.Errorf("failed to create kubernetes client: %w", err)})
		return
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	timeout := time.After(30 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			p.Send(ErrMsg{Err: ctx.Err()})
			return
		case <-timeout:
			p.Send(ErrMsg{Err: fmt.Errorf("timeout waiting for cluster to be ready")})
			return
		case <-ticker.C:
			msg, done := fetchCRDStatus(ctx, k8sClient, clusterName)
			p.Send(msg)
			if done {
				p.Send(DoneMsg{})
				return
			}
		}
	}
}

// fetchCRDStatus fetches the current CRD status and returns a message.
func fetchCRDStatus(ctx context.Context, k8sClient client.Client, clusterName string) (CRDStatusMsg, bool) {
	cluster := &k8znerv1alpha1.K8znerCluster{}
	key := client.ObjectKey{
		Namespace: Namespace,
		Name:      clusterName,
	}

	if err := k8sClient.Get(ctx, key, cluster); err != nil {
		return CRDStatusMsg{}, false
	}

	lastReconcile := ""
	if cluster.Status.LastReconcileTime != nil {
		lastReconcile = time.Since(cluster.Status.LastReconcileTime.Time).Round(time.Second).String() + " ago"
	}

	msg := CRDStatusMsg{
		ClusterPhase:   cluster.Status.Phase,
		ProvPhase:      cluster.Status.ProvisioningPhase,
		Infrastructure: cluster.Status.Infrastructure,
		ControlPlanes:  cluster.Status.ControlPlanes,
		Workers:        cluster.Status.Workers,
		Addons:         cluster.Status.Addons,
		PhaseHistory:   cluster.Status.PhaseHistory,
		LastErrors:     cluster.Status.LastErrors,
		LastReconcile:  lastReconcile,
	}

	done := cluster.Status.ProvisioningPhase == k8znerv1alpha1.PhaseComplete &&
		cluster.Status.Phase == k8znerv1alpha1.ClusterPhaseRunning

	return msg, done
}
