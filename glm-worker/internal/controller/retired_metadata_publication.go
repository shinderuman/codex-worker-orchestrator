package controller

import "fmt"

func (s *Store) applyRetiredMetadataPublication(op ExecutionOperation, remote string) (string, error) {
	p := op.Publication
	effect, found := publicationGuardEffect(op.Transition.Effects, MutationSurfaceHistory, p.Policy.Remote+":"+p.Policy.RemoteRef)
	if !found || effect.ExpectedOld != p.RemoteOID || effect.ExpectedNew != p.NewTip {
		return "", fmt.Errorf("parent metadata publication lacks exact remote authority")
	}
	if remote == p.NewTip {
		return remote, nil
	}
	if remote != p.RemoteOID {
		return "", s.abortRacedPublication(op, remote)
	}
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, p.Policy.LocalRef)
	if err != nil {
		return "", err
	}
	if !exists || local != p.NewTip {
		return "", fmt.Errorf("parent metadata publication local ref differs")
	}
	_, pushErr := runGitBinary(s.identity.PrimaryRoot, nil, "push", "--porcelain", p.Policy.Remote, p.Policy.LocalRef+":"+p.Policy.RemoteRef)
	remote, err = observePublicationRemote(s.identity.PrimaryRoot, p.Policy)
	if err != nil {
		return "", err
	}
	if remote != p.NewTip {
		return "", fmt.Errorf("parent metadata publication not proven: %w", pushErr)
	}
	return remote, nil
}
