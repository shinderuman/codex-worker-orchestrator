package observationexec

import "context"

func isolatedGoTestEnv(tempRoot string) ([]string, error) {
	return isolatedGoTestEnvContext(context.Background(), tempRoot)
}
