package cmd

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Manage Vitastor nodes",
}

var nodeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List VitastorNode resources",
	RunE:  runNodeList,
}

var nodeNooutCmd = &cobra.Command{
	Use:   "noout <node-name> <true|false>",
	Short: "Set noout flag on a VitastorNode (propagates to all its OSDs)",
	Args:  cobra.ExactArgs(2),
	RunE:  runNodeNoout,
}

func init() {
	nodeCmd.AddCommand(nodeListCmd)
	nodeCmd.AddCommand(nodeNooutCmd)
}

func runNodeList(cmd *cobra.Command, args []string) error {
	nodeList := &controlv2.VitastorNodeList{}
	if err := k8sClient.List(appCtx, nodeList); err != nil {
		return fmt.Errorf("listing nodes: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "NAME\tNOOUT\tWEIGHT\tCLUSTER")
	for _, node := range nodeList.Items {
		cluster := node.Labels["control.vitastor.io/cluster"]
		fmt.Fprintf(w, "%s\t%v\t%s\t%s\n",
			node.Name,
			node.Spec.NoOut,
			node.Spec.Weight,
			cluster,
		)
	}
	return nil
}

func runNodeNoout(cmd *cobra.Command, args []string) error {
	nodeName, valueStr := args[0], args[1]
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return fmt.Errorf("invalid boolean value %q: %w", valueStr, err)
	}

	node := &controlv2.VitastorNode{}
	if err := k8sClient.Get(appCtx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return fmt.Errorf("getting node %s: %w", nodeName, err)
	}

	patch := client.MergeFrom(node.DeepCopy())
	node.Spec.NoOut = value
	if err := k8sClient.Patch(appCtx, node, patch); err != nil {
		return fmt.Errorf("patching node: %w", err)
	}
	fmt.Printf("Node %s noout set to %v. The operator will propagate to all OSDs on this node.\n", nodeName, value)
	return nil
}
