//go:build !linux && !darwin

package controller

import (
	"fmt"
	"os"
	"time"
)

func readValidationFileNoFollow(rootPath, runID, filename string, read func(*os.File) ([]byte, error)) ([]byte, time.Time, error) {
	return nil, time.Time{}, fmt.Errorf("post-install validation evidence requires a no-follow filesystem reader")
}
