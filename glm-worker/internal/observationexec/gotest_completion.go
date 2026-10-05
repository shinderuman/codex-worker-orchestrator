package observationexec

func classifyCompletedIsolatedGoTestOutcome(runErr error) GoTestOutcome {
	if runErr == nil {
		return GoTestOutcome{Status: StatusPass, ExitCode: 0, ExitSource: exitSourceTarget}
	}
	return GoTestOutcome{Status: StatusFail, ExitCode: goTestExitCode(runErr), ExitSource: exitSourceTarget}
}
