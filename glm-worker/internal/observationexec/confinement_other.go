//go:build !darwin

package observationexec

import (
	"context"
	"fmt"
)

const confinementSupportCondition = "macOSでsandbox-execが利用可能なこと"

func ConfinementAdmission() error {
	return fmt.Errorf("go-test executorのOS書込confinementがこのplatformでは利用できません(support条件: %s)。targetは起動しません", confinementSupportCondition)
}

func confinedLaunchArgs(_ string, _ []string) ([]string, error) {
	return nil, ConfinementAdmission()
}

func ConfinementPreflight(writeRoot string) error {
	return ConfinementPreflightContext(context.Background(), writeRoot)
}

func ConfinementPreflightContext(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ConfinementAdmission()
}
