//go:build !unix

package installlock

import (
	"fmt"
	"os"
)

func Directory() (string, error) {
	return "", fmt.Errorf("install lifecycle requires Unix file locking")
}

func Acquire(_ *os.File) error {
	return fmt.Errorf("install lifecycle requires Unix file locking")
}
