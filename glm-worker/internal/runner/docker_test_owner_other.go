//go:build !unix

package runner

import (
	"errors"
	"os"
)

func openStagingRequestFile(_, _ string) (*os.File, error) {
	return nil, errors.ErrUnsupported
}

func stagingFileIdentity(os.FileInfo) (stagingDirIdentity, bool) {
	return stagingDirIdentity{}, false
}

func stagingDirFileIdentity(*os.File) (stagingDirIdentity, bool) {
	return stagingDirIdentity{}, false
}

func readStagingDirentsFD(*os.File) ([]stagingDirent, error) {
	return nil, errors.ErrUnsupported
}

func createStagingResponseFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func pinStagingDirIdentity(string) (stagingDirIdentity, bool) {
	return stagingDirIdentity{}, false
}
