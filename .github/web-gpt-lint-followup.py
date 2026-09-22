from pathlib import Path


def replace(path, old, new, count=1):
    p = Path(path)
    text = p.read_text()
    found = text.count(old)
    if found != count:
        raise SystemExit(f"{path}: expected {count} occurrence(s), found {found}: {old[:100]!r}")
    p.write_text(text.replace(old, new, count))


replace(
    "glm-worker/internal/harnesslint/forward_only_shell_state.go",
    '''func shellStateKindForVariable(lines []string, variable string) string {
\tfor index := 0; index < len(lines); index++ {
\t\tselector := shellCaseVariablePattern.FindStringSubmatch(strings.TrimSpace(lines[index]))
\t\tif len(selector) != 2 || selector[1] != variable {
\t\t\tcontinue
\t\t}
\t\tfor arm := index; arm < len(lines); arm++ {
\t\t\tmatch := shellStateKindPattern.FindStringSubmatch(lines[arm])
\t\t\tif len(match) == 4 {
\t\t\t\tfor _, kind := range match[1:] {
\t\t\t\t\tif kind != "" {
\t\t\t\t\t\treturn kind
\t\t\t\t\t}
\t\t\t\t}
\t\t\t}
\t\t\tif strings.Contains(lines[arm], ";;") {
\t\t\t\tbreak
\t\t\t}
\t\t}
\t}
\treturn ""
}
''',
    '''func shellStateKindForVariable(lines []string, variable string) string {
\tfor index, line := range lines {
\t\tif shellCaseVariable(line) != variable {
\t\t\tcontinue
\t\t}
\t\treturn shellStateKindForArm(lines, index)
\t}
\treturn ""
}

func shellCaseVariable(line string) string {
\tmatch := shellCaseVariablePattern.FindStringSubmatch(strings.TrimSpace(line))
\tif len(match) != 2 {
\t\treturn ""
\t}
\treturn match[1]
}

func shellStateKindForArm(lines []string, index int) string {
\tfor arm := index; arm < len(lines); arm++ {
\t\tif kind := shellStateKindFromLine(lines[arm]); kind != "" {
\t\t\treturn kind
\t\t}
\t\tif strings.Contains(lines[arm], ";;") {
\t\t\treturn ""
\t\t}
\t}
\treturn ""
}

func shellStateKindFromLine(line string) string {
\tmatch := shellStateKindPattern.FindStringSubmatch(line)
\tif len(match) != 4 {
\t\treturn ""
\t}
\tfor _, kind := range match[1:] {
\t\tif kind != "" {
\t\t\treturn kind
\t\t}
\t}
\treturn ""
}
''',
)

replace(
    "glm-worker/internal/parentactioncmd/publication_pretool.go",
    '''func publicationGitSubcommand(argv []publicationShellWord) string {
\tfor index := 0; index < len(argv); index++ {
\t\tvalue := argv[index].Value
\t\tif value == "--" {
\t\t\tif index+1 < len(argv) {
\t\t\t\treturn argv[index+1].Value
\t\t\t}
\t\t\treturn ""
\t\t}
\t\tswitch value {
\t\tcase "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env":
\t\t\tindex++
\t\t\tcontinue
\t\t}
\t\tif strings.HasPrefix(value, "-C") && value != "-C" || strings.HasPrefix(value, "-c") && value != "-c" ||
\t\t\tstrings.HasPrefix(value, "--git-dir=") || strings.HasPrefix(value, "--work-tree=") ||
\t\t\tstrings.HasPrefix(value, "--namespace=") || strings.HasPrefix(value, "--super-prefix=") || strings.HasPrefix(value, "--config-env=") {
\t\t\tcontinue
\t\t}
\t\tif strings.HasPrefix(value, "-") {
\t\t\tcontinue
\t\t}
\t\treturn value
\t}
\treturn ""
}
''',
    '''func publicationGitSubcommand(argv []publicationShellWord) string {
\tfor index := 0; index < len(argv); index++ {
\t\tvalue := argv[index].Value
\t\tif value == "--" {
\t\t\treturn publicationGitSubcommandAfterSeparator(argv, index)
\t\t}
\t\tif publicationGitGlobalOptionConsumesValue(value) {
\t\t\tindex++
\t\t\tcontinue
\t\t}
\t\tif publicationGitInlineGlobalOption(value) || strings.HasPrefix(value, "-") {
\t\t\tcontinue
\t\t}
\t\treturn value
\t}
\treturn ""
}

func publicationGitSubcommandAfterSeparator(argv []publicationShellWord, index int) string {
\tif index+1 >= len(argv) {
\t\treturn ""
\t}
\treturn argv[index+1].Value
}

func publicationGitGlobalOptionConsumesValue(value string) bool {
\tswitch value {
\tcase "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env":
\t\treturn true
\tdefault:
\t\treturn false
\t}
}

func publicationGitInlineGlobalOption(value string) bool {
\tfor _, prefix := range []string{"-C", "-c", "--git-dir=", "--work-tree=", "--namespace=", "--super-prefix=", "--config-env="} {
\t\tif strings.HasPrefix(value, prefix) {
\t\t\treturn true
\t\t}
\t}
\treturn false
}
''',
)

replace("scripts/manage-pull-hook.sh", "had_active=0\n\n", "")
replace("scripts/manage-pull-hook.sh", "\thad_active=0\n", "")
replace("scripts/manage-pull-hook.sh", "\t\thad_active=1\n", "")
