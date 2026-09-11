from pathlib import Path

path = Path('install.sh')
text = path.read_text()
start = text.index('copy_file() {\n')
end = text.index('\nbuild_binaries() {\n', start)
replacement = '''install_codex_configuration() {
\tbuild_dir=$1
\t"$build_dir/codex-install" --repo-root "$repo_root" --codex-dir "$codex_dir"
}
'''
text = text[:start] + replacement + text[end:]
old = '''\t\tgo build -buildvcs=false -trimpath -o "$build_dir/glm-codex-context" ./cmd/glm-codex-context
\t\tgo build -buildvcs=false -trimpath -o "$build_dir/commentlint" ./cmd/commentlint
'''
new = '''\t\tgo build -buildvcs=false -trimpath -o "$build_dir/glm-codex-context" ./cmd/glm-codex-context
\t\tgo build -buildvcs=false -trimpath -o "$build_dir/codex-install" ./cmd/codex-install
\t\tgo build -buildvcs=false -trimpath -o "$build_dir/commentlint" ./cmd/commentlint
'''
if old not in text:
    raise SystemExit('build helper anchor missing')
text = text.replace(old, new, 1)
old = '''install_binaries "$build_dir"
install_codex_files
merge_codex_config
merge_claude_settings "$build_dir"
'''
new = '''install_binaries "$build_dir"
install_codex_configuration "$build_dir"
merge_claude_settings "$build_dir"
'''
if old not in text:
    raise SystemExit('installer call anchor missing')
text = text.replace(old, new, 1)
path.write_text(text)

path = Path('glm-worker/internal/codexinstall/config.go')
text = path.read_text()
old = '''\t\tplan.Next = replaceAssignmentLine(data, current.Index, "")
\t\tplan.Changed = !strings.EqualFold(string(plan.Next), string(data)) || string(plan.Next) != string(data)
'''
new = '''\t\tplan.Next = replaceAssignmentLine(data, current.Index, "")
\t\tplan.Changed = string(plan.Next) != string(data)
'''
if old not in text:
    raise SystemExit('config changed anchor missing')
path.write_text(text.replace(old, new, 1))
