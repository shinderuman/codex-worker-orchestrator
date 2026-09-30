package harnesslint

import "testing"

func TestForwardOnlyAuthorityFallbackRejectsUnavailableControllerStateStoreMutation(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/dispatch.go"
	writeFixture(t, root, path, `package example

type Config struct{}
type RepositoryController struct{}
type StateStore struct{}

func execute(controller *RepositoryController, cfg Config) error {
	if controller == nil {
		return executeStateBacked(cfg)
	}
	return controller.Execute()
}

func executeStateBacked(cfg Config) error {
	st, err := NewStateStore(cfg)
	if err != nil { return err }
	return st.SaveState()
}

func NewStateStore(Config) (*StateStore, error) { return nil, nil }
func (*StateStore) SaveState() error { return nil }
func (*RepositoryController) Execute() error { return nil }
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlyAuthorityFallbackRejectsPositiveControllerGuardFallthrough(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/example/dispatch.go"
	writeFixture(t, root, path, `package example

type Config struct{}
type repositoryHarnessDecision struct { Active bool }
type StateStore struct{}

func execute(cfg Config) error {
	decision, err := evaluateRepositoryHarness(cfg)
	if err != nil { return err }
	if decision.Active {
		return executeController(cfg)
	}
	return executeStateBacked(cfg)
}

func evaluateRepositoryHarness(Config) (repositoryHarnessDecision, error) {
	return repositoryHarnessDecision{Active: true}, nil
}
func executeController(Config) error { return nil }
func executeStateBacked(cfg Config) error {
	st, err := NewStateStore(cfg)
	if err != nil { return err }
	lock, err := AcquireRepoLock(st.LockPath())
	if err != nil { return err }
	defer lock.Close()
	if err := admitParentCommand(st); err != nil { return err }
	return nil
}

func NewStateStore(Config) (*StateStore, error) { return nil, nil }
func (*StateStore) LockPath() string { return "" }
type repoLock struct{}
func AcquireRepoLock(string) (*repoLock, error) { return nil, nil }
func (*repoLock) Close() error { return nil }
func admitParentCommand(*StateStore) error { return nil }
`)
	requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, path)
}

func TestForwardOnlyAuthorityFallbackAllowsReadOnlyStateStoreFallback(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/read.go", `package example

type Config struct{}
type RepositoryController struct{}
type StateStore struct{}

func read(controller *RepositoryController, cfg Config) (string, error) {
	if controller == nil {
		return readStateBacked(cfg)
	}
	return controller.Read()
}

func readStateBacked(cfg Config) (string, error) {
	st, err := NewStateStore(cfg)
	if err != nil { return "", err }
	return st.LoadState()
}

func NewStateStore(Config) (*StateStore, error) { return nil, nil }
func (*StateStore) LoadState() (string, error) { return "", nil }
func (*RepositoryController) Read() (string, error) { return "", nil }
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlyAuthorityFallbackAllowsCurrentControllerDelegation(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/controller.go", `package example

type RepositoryController struct{}
var errControllerUnavailable error

func execute(controller *RepositoryController) error {
	if controller == nil {
		return errControllerUnavailable
	}
	return executeCurrentController(controller)
}

func executeCurrentController(controller *RepositoryController) error {
	return controller.RecordAdmittedMutation()
}

func (*RepositoryController) RecordAdmittedMutation() error { return nil }
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}

func TestForwardOnlyAuthorityFallbackAllowsUnrelatedMutatingFallback(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/example/generic.go", `package example

type Config struct{}
type StateStore struct{}

func execute(useFallback bool, cfg Config) error {
	if useFallback {
		return stateBacked(cfg)
	}
	return nil
}

func stateBacked(cfg Config) error {
	st, err := NewStateStore(cfg)
	if err != nil { return err }
	return st.SaveState()
}

func NewStateStore(Config) (*StateStore, error) { return nil, nil }
func (*StateStore) SaveState() error { return nil }
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}
