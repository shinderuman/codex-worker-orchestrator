package harnesslint

import "fmt"

const (
	MachineReportBundleAnalysis = "bundle-analysis-index"
	MachineReportBundleManifest = "bundle-manifest"
	MachineReportBundleReceipt  = "bundle-receipt"
	MachineReportTestImpact     = "test-impact"
	MachineReportRepoSearch     = "repo-search"
	MachineReportModelRouting   = "model-routing"
)

var machineReportBudgets = map[string]int{
	MachineReportBundleAnalysis: 12 * 1024,
	MachineReportBundleManifest: 4 * 1024,
	MachineReportBundleReceipt:  2 * 1024,
	MachineReportTestImpact:     6 * 1024,
	MachineReportRepoSearch:     8 * 1024,
	MachineReportModelRouting:   8 * 1024,
}

func MachineReportBudget(surface string) (int, bool) {
	limit, ok := machineReportBudgets[surface]
	return limit, ok
}

func CheckMachineReportBudget(surface string, data []byte) error {
	limit, ok := MachineReportBudget(surface)
	if !ok {
		return fmt.Errorf("unknown machine report surface %q", surface)
	}
	if len(data) <= limit {
		return nil
	}
	return fmt.Errorf("machine report %s is %d bytes, budget is %d", surface, len(data), limit)
}
