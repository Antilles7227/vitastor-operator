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

var osdNodeFilter string

var osdCmd = &cobra.Command{
	Use:   "osd",
	Short: "Manage Vitastor OSDs",
}

var osdListCmd = &cobra.Command{
	Use:   "list",
	Short: "List VitastorOSD resources",
	RunE:  runOSDList,
}

var osdNooutCmd = &cobra.Command{
	Use:   "noout <osd-name> <true|false>",
	Short: "Set noout flag on a VitastorOSD",
	Long:  "When noout=true, Vitastor will not rebalance data away from this OSD when it goes down.",
	Args:  cobra.ExactArgs(2),
	RunE:  runOSDNoout,
}

var osdWeightCmd = &cobra.Command{
	Use:   "weight <osd-name> <value>",
	Short: "Set weight on a VitastorOSD",
	Long:  "Weight controls data placement. Use 0 to migrate all data away before decommissioning.",
	Args:  cobra.ExactArgs(2),
	RunE:  runOSDWeight,
}

func init() {
	osdListCmd.Flags().StringVar(&osdNodeFilter, "node", "", "Filter OSDs by node name")
	osdCmd.AddCommand(osdListCmd)
	osdCmd.AddCommand(osdNooutCmd)
	osdCmd.AddCommand(osdWeightCmd)
}

func runOSDList(cmd *cobra.Command, args []string) error {
	osdList := &controlv2.VitastorOSDList{}
	listOpts := []client.ListOption{}
	if osdNodeFilter != "" {
		listOpts = append(listOpts, client.MatchingLabels{"control.vitastor.io/node": osdNodeFilter})
	}
	if err := k8sClient.List(appCtx, osdList, listOpts...); err != nil {
		return fmt.Errorf("listing OSDs: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "NAME\tID\tSTATE\tNOOUT\tWEIGHT\tNODE")
	for _, osd := range osdList.Items {
		nodeName := osd.Labels["control.vitastor.io/node"]
		fmt.Fprintf(w, "%s\t%d\t%s\t%v\t%s\t%s\n",
			osd.Name,
			osd.Spec.Id,
			osd.Status.State,
			osd.Spec.NoOut,
			osd.Spec.Weight,
			nodeName,
		)
	}
	return nil
}

func runOSDNoout(cmd *cobra.Command, args []string) error {
	osdName, valueStr := args[0], args[1]
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return fmt.Errorf("invalid boolean value %q: %w", valueStr, err)
	}

	osd := &controlv2.VitastorOSD{}
	if err := k8sClient.Get(appCtx, client.ObjectKey{Name: osdName}, osd); err != nil {
		return fmt.Errorf("getting OSD %s: %w", osdName, err)
	}

	patch := client.MergeFrom(osd.DeepCopy())
	osd.Spec.NoOut = value
	if err := k8sClient.Patch(appCtx, osd, patch); err != nil {
		return fmt.Errorf("patching OSD: %w", err)
	}
	fmt.Printf("OSD %s noout set to %v\n", osdName, value)
	return nil
}

func runOSDWeight(cmd *cobra.Command, args []string) error {
	osdName, weightStr := args[0], args[1]
	// Validate it's a valid float
	if _, err := strconv.ParseFloat(weightStr, 64); err != nil {
		return fmt.Errorf("invalid weight value %q: must be a number (e.g. 1.0, 0.5, 0)", weightStr)
	}

	osd := &controlv2.VitastorOSD{}
	if err := k8sClient.Get(appCtx, client.ObjectKey{Name: osdName}, osd); err != nil {
		return fmt.Errorf("getting OSD %s: %w", osdName, err)
	}

	patch := client.MergeFrom(osd.DeepCopy())
	osd.Spec.Weight = weightStr
	if err := k8sClient.Patch(appCtx, osd, patch); err != nil {
		return fmt.Errorf("patching OSD: %w", err)
	}
	fmt.Printf("OSD %s weight set to %s\n", osdName, weightStr)
	return nil
}
