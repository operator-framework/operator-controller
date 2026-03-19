package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(ocv1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(operatorsv1.AddToScheme(scheme))
	utilruntime.Must(operatorsv1alpha1.AddToScheme(scheme))
}

var (
	subscriptionName      string
	subscriptionNamespace string
	clusterExtensionName  string
	installNamespace      string
	kubeconfig            string
	autoApprove           bool
)

var rootCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate OLMv0-managed operators to OLMv1",
	Long: `migrate is a CLI tool for migrating operators managed by OLMv0 (Subscription/CSV)
to OLMv1 (ClusterExtension/ClusterExtensionRevision).

It profiles the existing installation, validates compatibility, collects operator resources,
and creates the corresponding OLMv1 resources for a zero-downtime migration.

Examples:
  # Migrate a single operator (interactive)
  migrate -s my-operator -n operators

  # Migrate a single operator (non-interactive)
  migrate -s my-operator -n operators -y

  # Migrate all eligible operators
  migrate all

  # Check readiness and compatibility only
  migrate check -s my-operator -n operators

  # Gather migration info without creating resources
  migrate gather -s my-operator -n operators`,
	RunE: runMigrate,
}

// addSubscriptionFlags adds -s/-n and related flags to a command.
func addSubscriptionFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&subscriptionName, "subscription", "s", "", "Name of the OLMv0 Subscription to migrate (required)")
	cmd.Flags().StringVarP(&subscriptionNamespace, "namespace", "n", "", "Namespace of the Subscription (required)")
	cmd.Flags().StringVar(&clusterExtensionName, "ce-name", "", "Name for the ClusterExtension (default: subscription name)")
	cmd.Flags().StringVar(&installNamespace, "install-namespace", "", "Install namespace for the ClusterExtension (default: subscription namespace)")
	_ = cmd.MarkFlagRequired("subscription")
	_ = cmd.MarkFlagRequired("namespace")
}

func init() {
	// Global flags (available to all commands)
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: KUBECONFIG env or ~/.kube/config)")
	rootCmd.PersistentFlags().BoolVarP(&autoApprove, "yes", "y", false, "Skip confirmation prompts")

	// Root command needs subscription flags
	addSubscriptionFlags(rootCmd)

	// Subcommands
	addSubscriptionFlags(checkCmd)
	addSubscriptionFlags(gatherCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(gatherCmd)
	rootCmd.AddCommand(allCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newClient() (client.Client, *rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get REST config: %w", err)
	}

	c, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create client: %w", err)
	}
	return c, restConfig, nil
}
