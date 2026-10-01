package app

import (
	"io"
	"os"
)

func enterStdinRawMode(stdin io.Reader) (restore func() error, applied bool, err error) {
	file, ok := stdin.(*os.File)
	if !ok {
		return noopStdinRestore, false, nil
	}
	return setStdinFileRaw(file)
}

func noopStdinRestore() error {
	return nil
}
