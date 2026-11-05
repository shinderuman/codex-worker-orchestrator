package main

import (
    "errors"
    "flag"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"

    "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudesettings"
    "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
    "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
    "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

func main() {
    repo := flag.String("repo-root", "", "repository root")
    bin := flag.String("bin-dir", "", "installed binary directory")
    codex := flag.String("codex-dir", "", "Codex config directory")
    flag.Parse()
    if flag.NArg() != 0 || *repo == "" || *bin == "" || *codex == "" {
        fmt.Fprintln(os.Stderr, "usage: uninstall -repo-root PATH -bin-dir PATH -codex-dir PATH")
        os.Exit(2)
    }
    if err := run(*repo, *bin, *codex); err != nil {
        fmt.Fprintln(os.Stderr, "uninstall incomplete:", err)
        os.Exit(1)
    }
    fmt.Println("uninstall complete")
}

func run(repoRoot, binDir, codexDir string) error {
    home, err := os.UserHomeDir()
    if err != nil { return err }
    location, err := claudesettings.Resolve(home, os.Getenv("CLAUDE_CONFIG_DIR"), os.Getenv("CLAUDE_SETTINGS_FILE"))
    if err != nil { return fmt.Errorf("resolve Claude settings: %w", err) }

    // Reject symlinks in existing path components before any owner mutates its files.
    for _, path := range []string{repoRoot, binDir, codexDir, location.SettingsPath} {
        if err := rejectSymlinkComponents(path); err != nil { return err }
    }

    var failures []error
    attempt := func(label string, fn func() error) {
        if err := fn(); err != nil {
            failures = append(failures, fmt.Errorf("%s: %w", label, err))
            fmt.Fprintf(os.Stderr, "uninstall %s: failed: %v\n", label, err)
        } else {
            fmt.Printf("uninstall %s: completed or unchanged\n", label)
        }
    }
    // Retire hook while its guard is still present; all owners retain their
    // respective state if another owner fails.
    attempt("repository hooks", func() error {
        cmd := exec.Command("sh", filepath.Join(repoRoot, "scripts", "manage-pull-hook.sh"),
            "retire", repoRoot, filepath.Join(binDir, "glm-publication-guard"))
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr
        return cmd.Run()
    })
    attempt("Codex", func() error { return codexinstall.Retire(codexDir, os.Stdout) })
    attempt("Claude", func() error { return settingsmerge.RetireFiles(location.SettingsPath) })
    attempt("CLI binaries", func() error {
        results, err := cliinstall.Retire(binDir)
        for _, result := range results { fmt.Printf("%s: %s\n", result.Name, result.Status) }
        return err
    })
    return errors.Join(failures...)
}

func rejectSymlinkComponents(path string) error {
    absolute, err := filepath.Abs(path)
    if err != nil { return err }
    current := string(filepath.Separator)
    volume := filepath.VolumeName(absolute)
    if volume != "" { current = volume + string(filepath.Separator) }
    relative := absolute[len(current):]
    for _, component := range filepath.SplitList("") { _ = component }
    for _, component := range splitPath(relative) {
        current = filepath.Join(current, component)
        info, err := os.Lstat(current)
        if os.IsNotExist(err) { continue }
        if err != nil { return err }
        if info.Mode()&os.ModeSymlink != 0 { return fmt.Errorf("refusing symlink path component: %s", current) }
    }
    return nil
}

func splitPath(path string) []string {
    var parts []string
    for path != "" && path != "." {
        dir, file := filepath.Split(path)
        if file != "" { parts = append([]string{file}, parts...) }
        path = filepath.Clean(dir)
        if path == "." { break }
    }
    return parts
}
