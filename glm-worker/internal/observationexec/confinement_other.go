//go:build !darwin

package observationexec

import "fmt"

const confinementSupportCondition = "macOSでsandbox-execが利用可能なこと"

func ConfinementAdmission() error {
	return fmt.Errorf("go-test executorのOS書込confinementがこのplatformでは利用できません(support条件: %s)。targetは起動しません", confinementSupportCondition)
}

func confinedLaunchArgs(_ string, _ []string) ([]string, error) {
	return nil, ConfinementAdmission()
}

func ConfinementPreflight(_ string) error {
	return ConfinementAdmission()
}
