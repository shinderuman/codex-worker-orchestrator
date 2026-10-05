package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func (s *Store) validateExecutionOperation(op ExecutionOperation) (executionOperationContract, error) {
	digest, err := executionOperationDigest(op)
	if err != nil {
		return executionOperationContract{}, err
	}
	if op.Transition.OperationDigest == "" || op.Transition.OperationDigest != digest {
		return executionOperationContract{}, fmt.Errorf("execution operation content integrity failed")
	}
	contract, err := s.executionOperationContract(op.Transition.Kind)
	if err != nil {
		return executionOperationContract{}, err
	}
	if contract.validate == nil || contract.apply == nil || contract.verifyCommitted == nil {
		return executionOperationContract{}, fmt.Errorf("execution operation contract is incomplete")
	}
	if err := validateExecutionOperationEffects(op, contract.effects); err != nil {
		return executionOperationContract{}, err
	}
	if err := contract.validate(op); err != nil {
		return executionOperationContract{}, err
	}
	return contract, nil
}

func executionOperationDigest(op ExecutionOperation) (string, error) {
	op.Transition.OperationDigest = ""
	data, err := json.Marshal(op)
	return digestStrings("controller-execution-operation-v1", string(data)), err
}

func readExecutionRef(repo, ref string) (string, bool, error) {
	data, err := runGitBinary(repo, nil, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(data)), true, nil
}
