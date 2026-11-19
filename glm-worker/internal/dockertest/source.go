package dockertest

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func sourceArchive(ctx context.Context, root, temporary string, snapshot state.GitSnapshot) (string, string, error) {
	clone := filepath.Join(temporary, "git-copy")
	if _, err := output(ctx, temporary, "git", "clone", "--quiet", "--no-hardlinks", "--dissociate", "--no-checkout", root, clone); err != nil {
		return "", "", err
	}
	if _, err := output(ctx, clone, "git", "update-ref", "--no-deref", "HEAD", snapshot.Head); err != nil {
		return "", "", err
	}
	if err := copyRepositoryIndex(ctx, root, clone); err != nil {
		return "", "", err
	}
	paths, err := output(ctx, root, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(temporary, "source.tar")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", err
	}
	hash := sha256.New()
	writer := tar.NewWriter(io.MultiWriter(file, hash))
	writeErr := writeArchive(ctx, writer, root, clone, paths)
	closeErr := writer.Close()
	fileErr := file.Close()
	for _, err := range []error{writeErr, closeErr, fileErr} {
		if err != nil {
			_ = os.Remove(path)
			return "", "", err
		}
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(path)
		return "", "", err
	}
	return path, hex.EncodeToString(hash.Sum(nil)), nil
}

func copyRepositoryIndex(ctx context.Context, root, clone string) error {
	data, err := output(ctx, root, "git", "rev-parse", "--git-path", "index")
	if err != nil {
		return err
	}
	index := strings.TrimSpace(string(data))
	if !filepath.IsAbs(index) {
		index = filepath.Join(root, index)
	}
	body, err := os.ReadFile(index)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(clone, ".git", "index"), body, 0o600); err != nil {
		return err
	}
	return copySharedIndexFiles(filepath.Dir(index), filepath.Join(clone, ".git"))
}

func copySharedIndexFiles(sourceDir, destinationDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "sharedindex.") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destinationDir, entry.Name()), body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func writeArchive(ctx context.Context, writer *tar.Writer, root, clone string, paths []byte) error {
	seen := map[string]bool{}
	for _, path := range strings.Split(string(paths), "\x00") {
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if strings.HasSuffix(path, "/") {
			if err := archiveNestedRepository(ctx, writer, root, path); err != nil {
				return err
			}
			continue
		}
		if err := archiveEntry(ctx, writer, root, path, true); err != nil {
			return err
		}
	}
	walkErr := filepath.WalkDir(filepath.Join(clone, ".git"), func(path string, _ fs.DirEntry, err error) error {
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(clone, path)
		if err != nil {
			return err
		}
		return archiveEntry(ctx, writer, clone, relative, false)
	})
	if walkErr != nil {
		return walkErr
	}
	return ctx.Err()
}

func archiveNestedRepository(ctx context.Context, writer *tar.Writer, root, dir string) error {
	entries, err := state.NestedUntrackedRepositoryEntriesContext(ctx, root, dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := archiveEntry(ctx, writer, root, entry.Path, true); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func archiveEntry(ctx context.Context, writer *tar.Writer, root, path string, allowDeleted bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	header, err := archiveHeader(root, path, allowDeleted)
	if err != nil || header == nil {
		return err
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	if header.Typeflag != tar.TypeReg {
		return nil
	}
	file, err := os.Open(filepath.Join(root, path))
	if err != nil {
		return err
	}
	_, copyErr := copyArchive(ctx, writer, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

type archiveContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r archiveContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func copyArchive(ctx context.Context, writer io.Writer, reader io.Reader) (int64, error) {
	n, err := io.Copy(writer, archiveContextReader{ctx: ctx, reader: reader})
	if err != nil {
		return n, err
	}
	return n, ctx.Err()
}

func archiveHeader(root, path string, allowDeleted bool) (*tar.Header, error) {
	if filepath.IsAbs(path) || filepath.Clean(path) == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("source pathがrepository外です: %q", path)
	}
	absolute := filepath.Join(root, path)
	info, err := os.Lstat(absolute)
	if allowDeleted && os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if allowDeleted && info.IsDir() {
		return nil, fmt.Errorf("submoduleのsource転送は未対応です: %s", path)
	}
	link, err := archiveLink(root, absolute, info.Mode(), allowDeleted)
	if err != nil {
		return nil, err
	}
	header, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return nil, err
	}
	header.Name = filepath.ToSlash(path)
	return header, nil
}

func archiveLink(root, absolute string, mode fs.FileMode, enforceRoot bool) (string, error) {
	if mode&os.ModeSymlink == 0 {
		return "", nil
	}
	link, err := os.Readlink(absolute)
	if err != nil {
		return "", err
	}
	if !enforceRoot {
		return link, nil
	}
	target := link
	if filepath.IsAbs(target) {
		return "", fmt.Errorf("絶対path symlinkは転送できません: %s", absolute)
	}
	target = filepath.Join(filepath.Dir(absolute), target)
	relative, err := filepath.Rel(root, filepath.Clean(target))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repository外へのsymlinkは転送できません: %s", absolute)
	}
	return link, nil
}
