package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	kubeconfig string
	namespace  string
	k8sClient  client.Client
	appCtx     = context.Background()
)

var rootCmd = &cobra.Command{
	Use:   "kubectl-vitastor",
	Short: "kubectl plugin for managing Vitastor storage clusters",
	Long:  `kubectl-vitastor provides commands for managing Vitastor distributed storage in Kubernetes.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initClient()
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "Path to kubeconfig file")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (not used for cluster-scoped resources)")

	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(diskCmd)
	rootCmd.AddCommand(osdCmd)
	rootCmd.AddCommand(nodeCmd)
	rootCmd.AddCommand(poolCmd)
}

func initClient() error {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("adding clientgo scheme: %w", err)
	}
	if err := controlv2.AddToScheme(scheme); err != nil {
		return fmt.Errorf("adding controlv2 scheme: %w", err)
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %w", err)
	}

	k8sClient, err = client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}
	return nil
}
