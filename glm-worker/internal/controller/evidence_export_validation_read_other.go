//go:build !linux && !darwin

package controller

import (
	"fmt"
	"os"
)

func readValidationFileNoFollow(rootPath, runID, filename string, read func(*os.File) ([]byte, error)) ([]byte, error) {
	return nil, fmt.Errorf("post-install validation evidence requires a no-follow filesystem reader")
}
