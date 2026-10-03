package controller

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"

func Activate(cfg config.AppConfig) (Admission, error) {
	identity, err := ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return Admission{}, err
	}
	workspace, err := ResolveWorkspaceIdentity(cfg.RepoRoot, identity)
	if err != nil {
		return Admission{}, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		return Admission{}, err
	}
	store, err := Open(cfg)
	if err != nil {
		return Admission{}, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.LiveLeaseID != "" {
		authority, err := MutationAuthorityFromHead(head)
		if err != nil {
			return Admission{}, err
		}
		return store.AdmitMutationOrFailClosed(authority, workspace, snapshot)
	}
	authority, err := ResolveCommittedTaskAuthority(cfg.RepoRoot)
	if err != nil {
		return Admission{}, err
	}
	return store.BootstrapExecution(authority.Task, workspace, snapshot)
}
