package observationexec

import "context"

func RunIsolatedGoTest(input GoTestInput) GoTestOutcome {
	return RunIsolatedGoTestContext(context.Background(), input)
}

func isolatedGoTestEnv(tempRoot string) ([]string, error) {
	return isolatedGoTestEnvContext(context.Background(), tempRoot)
}
