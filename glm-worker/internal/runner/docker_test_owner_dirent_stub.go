//go:build unix && !darwin && !linux

package runner

import (
	"errors"
	"os"
)

func readStagingDirentChunk(*os.File, []byte) (int, error) {
	return 0, errors.ErrUnsupported
}

func stagingDirentRecordLength([]byte) (int, bool) {
	return 0, false
}

func stagingDirentRecord([]byte) (stagingDirent, bool) {
	return stagingDirent{}, false
}
