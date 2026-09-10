//go:build !unix

package app

type repoLockLease struct{}

func AcquireRepoLockLease(path string) (*repoLockLease, error) {
	return nil, ErrRepoLockLeaseUnavailable
}

func (l *repoLockLease) Release() {}
