package parentevidence

import (
	"go/build"
	"slices"
	"testing"
)

func TestDiffWorktreePlatformBuildSelection(t *testing.T) {
	owners := []string{"diff_fs_unix.go", "diff_fs_windows.go", "diff_fs_other.go"}
	for _, tc := range []struct {
		goos string
		want string
	}{
		{"linux", "diff_fs_unix.go"},
		{"darwin", "diff_fs_unix.go"},
		{"windows", "diff_fs_windows.go"},
		{"freebsd", "diff_fs_other.go"},
		{"openbsd", "diff_fs_other.go"},
		{"netbsd", "diff_fs_other.go"},
		{"illumos", "diff_fs_other.go"},
		{"solaris", "diff_fs_other.go"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			ctx := build.Default
			ctx.GOOS = tc.goos
			ctx.GOARCH = "amd64"
			pkg, err := ctx.ImportDir(".", 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, owner := range owners {
				if selected := slices.Contains(pkg.GoFiles, owner); selected != (owner == tc.want) {
					t.Errorf("source %s selected=%v, wanted %v; GoFiles=%v", owner, selected, owner == tc.want, pkg.GoFiles)
				}
			}
		})
	}
}
