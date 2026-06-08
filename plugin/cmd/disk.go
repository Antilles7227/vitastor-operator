package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	diskNodeFilter string
	osdPerDisk     int32
)

var diskCmd = &cobra.Command{
	Use:   "disk",
	Short: "Manage Vitastor disks",
}

var diskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List VitastorDisk resources",
	RunE:  runDiskList,
}

var diskPrepareCmd = &cobra.Command{
	Use:   "prepare <node> <device>",
	Short: "Prepare a disk for use as Vitastor OSD",
	Long:  "Sets the disk's desiredState to Prepared. The operator will partition and configure it.",
	Args:  cobra.ExactArgs(2),
	RunE:  runDiskPrepare,
}

var diskRemoveCmd = &cobra.Command{
	Use:   "remove <node> <device>",
	Short: "Decommission a disk from the Vitastor cluster",
	Long:  "Sets the disk's desiredState to Decommissioned. The operator will drain data and clean up.",
	Args:  cobra.ExactArgs(2),
	RunE:  runDiskRemove,
}

func init() {
	diskListCmd.Flags().StringVar(&diskNodeFilter, "node", "", "Filter disks by node name")
	diskPrepareCmd.Flags().Int32Var(&osdPerDisk, "osd-per-disk", 1, "Number of OSDs to create per disk")
	diskCmd.AddCommand(diskListCmd)
	diskCmd.AddCommand(diskPrepareCmd)
	diskCmd.AddCommand(diskRemoveCmd)
}

func runDiskList(cmd *cobra.Command, args []string) error {
	diskList := &controlv2.VitastorDiskList{}
	listOpts := []client.ListOption{}
	if diskNodeFilter != "" {
		listOpts = append(listOpts, client.MatchingLabels{"control.vitastor.io/node": diskNodeFilter})
	}
	if err := k8sClient.List(appCtx, diskList, listOpts...); err != nil {
		return fmt.Errorf("listing disks: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "NAME\tNODE\tDEVICE\tSTATE\tTYPE\tDESIRED STATE")
	for _, disk := range diskList.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			disk.Name,
			disk.Spec.NodeRef,
			disk.Spec.DevicePath,
			disk.Status.State,
			string(disk.Status.Type),
			string(disk.Spec.DesiredState),
		)
	}
	return nil
}

func findDisk(ctx context.Context, nodeName, devicePath string) (*controlv2.VitastorDisk, error) {
	diskList := &controlv2.VitastorDiskList{}
	if err := k8sClient.List(ctx, diskList); err != nil {
		return nil, fmt.Errorf("listing disks: %w", err)
	}
	for i := range diskList.Items {
		d := &diskList.Items[i]
		if d.Spec.NodeRef == nodeName && d.Spec.DevicePath == devicePath {
			return d, nil
		}
	}
	return nil, fmt.Errorf("disk not found: node=%s device=%s", nodeName, devicePath)
}

func runDiskPrepare(cmd *cobra.Command, args []string) error {
	nodeName, devicePath := args[0], args[1]
	disk, err := findDisk(appCtx, nodeName, devicePath)
	if err != nil {
		return err
	}

	patch := client.MergeFrom(disk.DeepCopy())
	disk.Spec.DesiredState = controlv2.DiskStatePrepared
	disk.Spec.DesiredOSDCount = osdPerDisk
	if err := k8sClient.Patch(appCtx, disk, patch); err != nil {
		return fmt.Errorf("patching disk: %w", err)
	}
	fmt.Printf("Disk %s on node %s set to Prepared (osd-per-disk=%s)\n",
		devicePath, nodeName, strconv.Itoa(int(osdPerDisk)))
	return nil
}

func runDiskRemove(cmd *cobra.Command, args []string) error {
	nodeName, devicePath := args[0], args[1]
	disk, err := findDisk(appCtx, nodeName, devicePath)
	if err != nil {
		return err
	}

	patch := client.MergeFrom(disk.DeepCopy())
	disk.Spec.DesiredState = controlv2.DiskStateDecommissioned
	if err := k8sClient.Patch(appCtx, disk, patch); err != nil {
		return fmt.Errorf("patching disk: %w", err)
	}
	fmt.Printf("Disk %s on node %s set to Decommissioned. The operator will drain data and clean up.\n",
		devicePath, nodeName)
	return nil
}
