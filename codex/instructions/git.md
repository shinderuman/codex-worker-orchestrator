# Git詳細規則
- `git diff` / `git show` / `git log`を`head` / `tail`等へパイプしない。

## Git metadataのsandbox実行境界

- `git status`・`git diff`・`git show`・`git log`・`git rev-parse`等、Git metadataを変更しないread-only commandは通常sandbox内で実行する。
- GLM worker/reviewerによる`git push`その他Git remote writeは禁止する。
