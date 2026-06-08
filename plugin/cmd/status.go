package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Vitastor cluster status overview",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	clusterList := &controlv2.VitastorClusterList{}
	if err := k8sClient.List(appCtx, clusterList); err != nil {
		return fmt.Errorf("listing clusters: %w", err)
	}

	if len(clusterList.Items) == 0 {
		fmt.Println("No VitastorCluster resources found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "CLUSTER\tNAMESPACE\tNODE LABEL\tNODES\tOSDs\tPOOLS")

	for _, cluster := range clusterList.Items {
		// Count nodes
		nodeList := &controlv2.VitastorNodeList{}
		_ = k8sClient.List(appCtx, nodeList, client.MatchingLabels{"control.vitastor.io/cluster": cluster.Name})

		// Count OSDs
		osdList := &controlv2.VitastorOSDList{}
		_ = k8sClient.List(appCtx, osdList, client.MatchingLabels{"control.vitastor.io/cluster": cluster.Name})

		// Count pools
		poolList := &controlv2.VitastorPoolList{}
		_ = k8sClient.List(appCtx, poolList)

		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\n",
			cluster.Name,
			cluster.Spec.VitastorClusterNamespace,
			cluster.Spec.VitastorNodeLabel,
			len(nodeList.Items),
			len(osdList.Items),
			len(poolList.Items),
		)
	}
	return nil
}
