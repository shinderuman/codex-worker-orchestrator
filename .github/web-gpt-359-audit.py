import re
import subprocess
from pathlib import Path

paths = subprocess.check_output(["git", "ls-files", "*.md"], text=True).splitlines()
print(f"TRACKED_MARKDOWN_COUNT={len(paths)}")
for path in paths:
    print(f"MD\t{path}")

patterns = {
    "git-sha": re.compile(r"\b[0-9a-f]{7,40}\b"),
    "mutable-current": re.compile(r"(?:現在の|current\s+)(?:branch|HEAD|main|state|status|task|boundary|version|SHA|commit)", re.I),
    "git-state": re.compile(r"\b(?:HEAD|dirty|worktree|branch)\b", re.I),
    "completion-snapshot": re.compile(r"(?:完了済|実装済|completed|completion|PASS|FAIL|成功|失敗)", re.I),
    "external-current": re.compile(r"(?:latest|最新版|current version|現在のversion|現行version)", re.I),
    "dated-heading": re.compile(r"^#{1,6}\s+20\d{2}[-/]\d{1,2}[-/]\d{1,2}"),
}

print("CANDIDATES_BEGIN")
for path in paths:
    for lineno, line in enumerate(Path(path).read_text(errors="replace").splitlines(), 1):
        hits = [name for name, pattern in patterns.items() if pattern.search(line)]
        if hits:
            clipped = line if len(line) <= 320 else line[:317] + "..."
            print(f"CANDIDATE\t{path}\t{lineno}\t{','.join(hits)}\t{clipped}")
print("CANDIDATES_END")
