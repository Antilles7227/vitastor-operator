package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	controlv2 "gitlab.com/Antilles7227/vitastor-operator/api/v2"
)

var poolCmd = &cobra.Command{
	Use:   "pool",
	Short: "Manage Vitastor pools",
}

var poolListCmd = &cobra.Command{
	Use:   "list",
	Short: "List VitastorPool resources with usage statistics",
	RunE:  runPoolList,
}

func init() {
	poolCmd.AddCommand(poolListCmd)
}

func runPoolList(cmd *cobra.Command, args []string) error {
	poolList := &controlv2.VitastorPoolList{}
	if err := k8sClient.List(appCtx, poolList); err != nil {
		return fmt.Errorf("listing pools: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "NAME\tSCHEME\tPG SIZE\tPG COUNT\tPOOL ID\tUSED\tAVAILABLE\tVITASTORFS")
	for _, pool := range poolList.Items {
		available := formatBytes(pool.Status.Available)
		used := pool.Status.UsedPercent
		if used == "" {
			used = "N/A"
		}
		if available == "" {
			available = "N/A"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\t%s\t%v\n",
			pool.Name,
			pool.Spec.Scheme,
			pool.Spec.PGSize,
			pool.Spec.PGCount,
			pool.Status.ID,
			used,
			available,
			pool.Spec.VitastorFS,
		)
	}
	return nil
}

func formatBytes(bytes int64) string {
	if bytes == 0 {
		return ""
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
