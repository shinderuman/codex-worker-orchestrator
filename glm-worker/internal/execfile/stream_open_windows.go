package execfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func openStreamFile(path string) (*os.File, error) {
	name, err := streamWindowsPath(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	access := uint32(syscall.FILE_APPEND_DATA | syscall.FILE_WRITE_ATTRIBUTES | syscall.STANDARD_RIGHTS_WRITE | syscall.SYNCHRONIZE)
	flags := uint32(syscall.FILE_ATTRIBUTE_NORMAL | syscall.FILE_FLAG_OPEN_REPARSE_POINT)
	handle, err := syscall.CreateFile(name, access, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, flags, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	kind, err := syscall.GetFileType(handle)
	if err == nil && kind != syscall.FILE_TYPE_DISK {
		err = errors.New("実行fileの一時fileが通常fileではありません")
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: errors.Join(err, syscall.CloseHandle(handle))}
	}
	return os.NewFile(uintptr(handle), path), nil
}

func streamWindowsPath(path string) (*uint16, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if len(path) >= 248 && !strings.HasPrefix(path, `\\?\`) && !strings.HasPrefix(path, `\\.\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + path[2:]
		} else {
			path = `\\?\` + path
		}
	}
	return syscall.UTF16PtrFromString(path)
}
