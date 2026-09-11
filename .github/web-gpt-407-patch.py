from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    target = Path(path)
    text = target.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one anchor, found {count}")
    target.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/codexcontext/context.go",
    """\texcluded, err := ensureGitExclude(root)\n\tif err != nil {\n\t\tif created {\n\t\t\tif removeErr := removeManagedConfig(configPath); removeErr != nil {\n\t\t\t\treturn Result{}, errors.Join(\n\t\t\t\t\tfmt.Errorf(\"configure local Git exclude: %w\", err),\n\t\t\t\t\tfmt.Errorf(\"rollback project config: %w\", removeErr),\n\t\t\t\t)\n\t\t\t}\n\t\t}\n\t\treturn Result{}, fmt.Errorf(\"configure local Git exclude: %w\", err)\n\t}\n""",
    """\tagentsCreated, err := enableProjectAgentsOverride(root)\n\tif err != nil {\n\t\tif created {\n\t\t\tif removeErr := removeManagedConfig(configPath); removeErr != nil {\n\t\t\t\treturn Result{}, errors.Join(err, fmt.Errorf(\"rollback project config: %w\", removeErr))\n\t\t\t}\n\t\t}\n\t\treturn Result{}, err\n\t}\n\texcluded, err := ensureGitExclude(root)\n\tif err != nil {\n\t\tif agentsCreated {\n\t\t\t_ = disableProjectAgentsOverride(root)\n\t\t}\n\t\tif created {\n\t\t\tif removeErr := removeManagedConfig(configPath); removeErr != nil {\n\t\t\t\treturn Result{}, errors.Join(\n\t\t\t\t\tfmt.Errorf(\"configure local Git exclude: %w\", err),\n\t\t\t\t\tfmt.Errorf(\"rollback project config: %w\", removeErr),\n\t\t\t\t)\n\t\t\t}\n\t\t}\n\t\treturn Result{}, fmt.Errorf(\"configure local Git exclude: %w\", err)\n\t}\n""",
)

replace_once(
    "glm-worker/internal/codexcontext/context.go",
    """func disable(root string) (Result, error) {\n\tconfigPath := filepath.Join(root, filepath.FromSlash(ProjectConfigRelativePath))\n""",
    """func disable(root string) (Result, error) {\n\tif err := validateProjectAgentsDisable(root); err != nil {\n\t\treturn Result{}, err\n\t}\n\tconfigPath := filepath.Join(root, filepath.FromSlash(ProjectConfigRelativePath))\n""",
)

replace_once(
    "glm-worker/internal/codexcontext/context.go",
    """\tif err := removeGitExclude(root); err != nil {\n\t\treturn Result{}, fmt.Errorf(\"remove local Git exclude: %w\", err)\n\t}\n""",
    """\tif err := disableProjectAgentsOverride(root); err != nil {\n\t\treturn Result{}, err\n\t}\n\tif err := removeGitExclude(root); err != nil {\n\t\treturn Result{}, fmt.Errorf(\"remove local Git exclude: %w\", err)\n\t}\n""",
)

replace_once(
    "glm-worker/internal/codexcontext/context.go",
    """\texcluded, err := gitExcluded(root)\n\tif err != nil {\n\t\treturn Result{}, err\n\t}\n\treturn Result{\n\t\tStatus:            state,\n""",
    """\tagentsState, err := projectAgentsOverrideState(root)\n\tif err != nil {\n\t\treturn Result{}, err\n\t}\n\tif agentsState == \"conflict\" || (state == \"enabled\") != (agentsState == \"enabled\") {\n\t\tstate = \"conflict\"\n\t\tdetail = \"project config and project-scoped AGENTS bootstrap ownership do not match\"\n\t}\n\texcluded, err := gitExcluded(root)\n\tif err != nil {\n\t\treturn Result{}, err\n\t}\n\treturn Result{\n\t\tStatus:            state,\n""",
)

replace_once(
    "glm-worker/internal/parentactioncmd/runtime_install.go",
    """\tcase sourcePath == \"codex/AGENTS.md\":\n\t\treturn filepath.Join(cfg.CodexConfigDir, \"AGENTS.md\"), true\n""",
    """\tcase sourcePath == \"codex/AGENTS.md\":\n\t\treturn filepath.Join(cfg.CodexConfigDir, \"instructions\", \"codex-worker-orchestrator.md\"), true\n""",
)

replace_once(
    "tests/install_smoke.sh",
    """printf '%s\\n' 'local_key = \"keep\"' >\"$home/.codex/config.toml\"\nprintf '%s\\n' '{\"permissions\":{\"allow\":[\"local\"]},\"env\":{\"LOCAL\":\"keep\",\"REMOVE_ME\":\"local\"}}' >\"$home/.claude/settings.json\"\n""",
    """printf '%s\\n' 'local_key = \"keep\"' >\"$home/.codex/config.toml\"\nprintf '%s\\n' '# user-owned global Codex instruction' >\"$home/.codex/AGENTS.md\"\nprintf '%s\\n' 'AGENTS.md' >\"$home/.codex/.codex-config-managed-files\"\nglobal_agents_hash=$(shasum -a 256 \"$home/.codex/AGENTS.md\")\nprintf '%s\\n' '{\"permissions\":{\"allow\":[\"local\"]},\"env\":{\"LOCAL\":\"keep\",\"REMOVE_ME\":\"local\"}}' >\"$home/.claude/settings.json\"\n""",
)

replace_once(
    "tests/install_smoke.sh",
    """test -f \"$home/.codex/AGENTS.md\"\ntest -f \"$home/.codex/rules/glm-worker.rules\"\n""",
    """test -f \"$home/.codex/AGENTS.md\"\nif [ \"$(shasum -a 256 \"$home/.codex/AGENTS.md\")\" != \"$global_agents_hash\" ]; then\n\tprintf '%s\\n' 'user-global AGENTS.md changed during install/upgrade' >&2\n\texit 1\nfi\nif grep -Fxq 'AGENTS.md' \"$home/.codex/.codex-config-managed-files\"; then\n\tprintf '%s\\n' 'user-global AGENTS.md remains installer-managed' >&2\n\texit 1\nfi\ntest -f \"$home/.codex/instructions/codex-worker-orchestrator.md\"\ncmp \"$repo/codex/AGENTS.md\" \"$home/.codex/instructions/codex-worker-orchestrator.md\"\ntest -f \"$home/.codex/rules/glm-worker.rules\"\n""",
)

replace_once(
    "tests/install_smoke.sh",
    """grep -Fxq 'plugins = false' \"$repo/.codex/config.toml\"\ngit -C \"$repo\" check-ignore -q -- .codex/config.toml\nif git -C \"$repo\" status --porcelain --untracked-files=all | grep -Fq '.codex/config.toml'; then\n""",
    """grep -Fxq 'plugins = false' \"$repo/.codex/config.toml\"\ntest -f \"$repo/AGENTS.override.md\"\ngrep -Fq \"$home/.codex/instructions/codex-worker-orchestrator.md\" \"$repo/AGENTS.override.md\"\ngrep -Fq 'repository rootにAGENTS.mdが存在する場合' \"$repo/AGENTS.override.md\"\ngit -C \"$repo\" check-ignore -q -- .codex/config.toml\ngit -C \"$repo\" check-ignore -q -- AGENTS.override.md\nif git -C \"$repo\" status --porcelain --untracked-files=all | grep -Eq '(.codex/config.toml|AGENTS.override.md)'; then\n""",
)

replace_once(
    "tests/install_smoke.sh",
    """grep -q '\"status\":\"disabled\"' \"$tmp/codex-context-disable.json\"\ntest ! -e \"$repo/.codex/config.toml\"\n""",
    """grep -q '\"status\":\"disabled\"' \"$tmp/codex-context-disable.json\"\ntest ! -e \"$repo/.codex/config.toml\"\ntest ! -e \"$repo/AGENTS.override.md\"\n""",
)

Path(".github/web-gpt-407-patch.py").unlink()
Path(".github/workflows/web-gpt-407-patch.yml").unlink()
