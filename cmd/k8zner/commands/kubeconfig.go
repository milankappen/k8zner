package commands

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/milankappen/k8zner/cmd/k8zner/handlers"
)

// Kubeconfig returns the command for generating scoped kubeconfigs.
//
// Required flags:
//
//	--read-only: generate a read-only kubeconfig (the only supported scope)
//
// Optional flags:
//
//	--kubeconfig: path to the admin kubeconfig (default: kubeconfig)
//	--output, -o: where to write the generated kubeconfig (default: kubeconfig-readonly)
//	--duration: token lifetime, e.g. 720h (default: 8760h = 1 year)
func Kubeconfig() *cobra.Command {
	var (
		readOnly      bool
		adminPath     string
		outputPath    string
		tokenDuration time.Duration
	)

	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Generate a scoped kubeconfig for the cluster",
		Long: `Generate a scoped kubeconfig for sharing cluster access.

Currently supports read-only access: a ServiceAccount token bound to the
built-in "view" ClusterRole. The "view" role grants no access to Secrets,
so the generated kubeconfig is safe to hand to teammates who need to
inspect workloads but must not read credentials.

Examples:
  # Generate a read-only kubeconfig valid for one year
  k8zner kubeconfig --read-only

  # Shorter-lived token, custom output path
  k8zner kubeconfig --read-only --duration 720h -o ./readonly.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return handlers.Kubeconfig(cmd.Context(), adminPath, outputPath, readOnly, tokenDuration)
		},
	}

	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Generate a read-only kubeconfig (required)")
	cmd.Flags().StringVar(&adminPath, "kubeconfig", "kubeconfig", "Path to the admin kubeconfig")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "kubeconfig-readonly", "Output path for the generated kubeconfig")
	cmd.Flags().DurationVar(&tokenDuration, "duration", 8760*time.Hour, "Token lifetime (e.g. 720h)")

	return cmd
}
