package installlock

import (
	"errors"
	"os"
)

var ErrBusy = errors.New("another install or uninstall is already running")

func With(operation func(*os.File) error) error {
	dir, err := Directory()
	if err != nil {
		return err
	}
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := Acquire(file); err != nil {
		return err
	}
	return operation(file)
}
