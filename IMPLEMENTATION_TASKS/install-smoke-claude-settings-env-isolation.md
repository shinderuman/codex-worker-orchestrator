# Task: install smoke Claude settings environment isolation

## Original instruction

````text
correctness defectはQuality Delta findingとして扱う。

新しく実装が必要なfindingも `priority:do` Issueへ起票せず、repository Taskとして管理する。
````

## Amendments

none

## Resolved references

- Dogfood bundle `50a2c845-a1e3-4fc1-91d4-254d377f242b` のworker / reviewerは、`tests/install_smoke.sh` が呼出元harnessから継承したambient `CLAUDE_CONFIG_DIR` とfixture内で明示する `CLAUDE_SETTINGS_FILE=$home/.claude/settings.json` のconflictにより失敗することを確認した
- 同smokeは `CLAUDE_CONFIG_DIR` をunsetするとPASSし、workerは同じ失敗がtask変更前sourceでも再現することを確認したため、`install-smoke-quality-tools-path-resolution` のregressionではない
- closed #489はinstaller/runtimeのClaude settings location authorityを統一し、`CLAUDE_CONFIG_DIR` と `CLAUDE_SETTINGS_FILE` が異なるsettings fileを指す場合にfail closedするproduction contractを導入した。本Findingではそのfail-closed resolver自体は正しく動作している
- current `tests/install_smoke.sh` はtemporary HOMEとfixture `CLAUDE_SETTINGS_FILE` を固定する一方、ambient `CLAUDE_CONFIG_DIR` を明示的にfixtureへ束縛または除去しないため、generic install smokeの結果がcaller environmentに依存する
- `tests/install_claude_settings_location_smoke.sh` のcustom-location fixtureは既に `CLAUDE_CONFIG_DIR=''` とcustom `CLAUDE_SETTINGS_FILE` を明示し、意図したlocation contractを独立に検証している

## Purpose

generic install smokeのClaude settings locationをtemporary fixtureへdeterministicに隔離し、caller / harnessのambient Claude location envによるfalse validation failureを防ぎつつ、productionのconflicting-location fail-closed contractを維持する。

## External feasibility

status: not-applicable

## Contract

- generic `tests/install_smoke.sh` はtemporary HOME fixtureで意図するClaude settings locationを、`CLAUDE_CONFIG_DIR` / `CLAUDE_SETTINGS_FILE` の両入力について矛盾なくdeterministicに定義する
- caller environmentに既存 `CLAUDE_CONFIG_DIR` または `CLAUDE_SETTINGS_FILE` があっても、generic fixtureの結果がambient valueに依存しない
- custom-location behaviorは専用 `install_claude_settings_location_smoke.sh` で引き続き検証し、generic smokeがcustom-location coverageを暗黙に代替しない
- production `claudesettings.Resolve` のconflicting-location errorと#489のcanonical path authorityは変更しない
- smoke fixture isolation以外のinstaller/runtime behaviorを変更しない

## Must not

- production resolverでconflicting `CLAUDE_CONFIG_DIR` / `CLAUDE_SETTINGS_FILE` を黙って無視・優先順位付けして通さない
- callerの実homeやClaude settingsへsmoke fixtureを書き込まない
- generic smokeのためにcustom-location coverageを削除しない
- quality-tools path resolutionなど別fixture責務を本taskへ混ぜない
- ambient env failureを単にCI/harness固有として無視しない

## Acceptance criteria

- ambient `CLAUDE_CONFIG_DIR` がfixture外のpathを指す状態でもgeneric `tests/install_smoke.sh` がtemporary fixtureだけを使用してdeterministicにPASSする
- ambient `CLAUDE_SETTINGS_FILE` が設定された状態でもgeneric smokeのfixture locationが意図どおり固定される
- production resolverのconflicting-location fixtureは引き続きfail closedする
- `tests/install_claude_settings_location_smoke.sh` のdefault/custom/conflict coverageを維持する
- user-owned real Claude settingsを変更しないことをfixtureで確認できる
- Repository Lintと関連install smokeがPASSする

## Historical invariants

- installer/runtimeのeffective Claude settings location authorityは#489で統一済みのproduction resolverを正とする
- smoke testはcaller environmentから隔離されたdeterministic fixtureであり、production conflict semanticsを弱める理由にしない

## Dependencies

none
