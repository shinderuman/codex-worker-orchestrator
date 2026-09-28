//go:build darwin

package observationexec

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const sandboxExecBin = "sandbox-exec"

const confinementSupportCondition = "macOSでsandbox-execが利用可能なこと"

func ConfinementAdmission() error {
	if _, err := exec.LookPath(sandboxExecBin); err != nil {
		return fmt.Errorf("go-test executorのOS書込confinementが利用できません(support条件: %s)。targetは起動しません", confinementSupportCondition)
	}
	return nil
}

func confinedLaunchArgs(writeRoot string, commandArgs []string) ([]string, error) {
	if err := ConfinementAdmission(); err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(writeRoot)
	if err != nil {
		return nil, fmt.Errorf("confinementの書込許可rootを解決できません: %w", err)
	}
	return append([]string{sandboxExecBin, "-p", confinementProfile(resolvedRoot)}, commandArgs...), nil
}

func confinementProfile(writeRoot string) string {
	var profile strings.Builder
	profile.WriteString("(version 1)\n")
	profile.WriteString("(deny file-write*)\n")
	profile.WriteString("(deny network*)\n")
	profile.WriteString(fmt.Sprintf("(allow file-write* (subpath %s))\n", seatbeltLiteral(writeRoot)))
	profile.WriteString(fmt.Sprintf("(allow file-write* (literal %s))\n", seatbeltLiteral("/dev/null")))
	profile.WriteString(fmt.Sprintf("(allow file-write* (literal %s))\n", seatbeltLiteral("/dev/dtracehelper")))
	return profile.String()
}

func ConfinementPreflight(writeRoot string) error {
	probe, err := confinedLaunchArgs(writeRoot, []string{"/usr/bin/true"})
	if err != nil {
		return err
	}
	command := exec.Command(probe[0], probe[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("confinement初期化に失敗したためtargetを起動しません: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func seatbeltLiteral(path string) string {
	escaped := strings.ReplaceAll(path, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
