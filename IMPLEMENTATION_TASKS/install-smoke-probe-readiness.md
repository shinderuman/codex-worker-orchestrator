# Task: post-install smoke probe readinessの欠落store扱い

## Original instruction

````text
smokeProbeSatisfiedがquality-gate run directory不在を「未充足」ではなくerrorとして伝播する問題を修正する。
````

## Amendments

none

## Resolved references

- CR-1422-1 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1422#discussion_r4404997278 · `glm-worker/internal/controller/publication_post_install_smoke.go:336-339`付近（`latestSmokeProbeRun` 473-479）
  原文: "** Major · Stability & Availability · Quick win** **Treat a missing run directory as \"not satisfied\" in `smokeProbeSatisfied`.** `latestSmokeProbeRun` returns the raw `os.ReadDir` error when the quality-gate run directory does not exist. The caller at lines 336-339 returns that error as-is. Sibling readers of the same directory treat a missing directory as an empty store. Here, a probe with no runs can fail the completion path instead of reporting that the probe is not yet satisfied. Treat `os.ErrNotExist` as no run, so the probe reports \"not satisfied\" and the operation can proceed to the first probe."

## Purpose

初回probe前のcompletion経路が欠落run directoryでfailしないようにする。

## External feasibility

status: not-applicable

## Contract

- `latestSmokeProbeRun`由来の`os.ErrNotExist`をnot-satisfied（errorなし）として扱う。他のread/decode errorは現行どおりerror伝播する。
- sibling reader（`finalValidationRunStore`等）の欠落扱いとの一致を回帰testで固定する。

## Must not

- 読取不能（unreadable）な既存recordを黙殺しない（`validation-evidence-unreadable-seal.md`の境界を侵さない）。

## Acceptance criteria

- run directory不在時にprobe未充足として次操作へ進む正caseと、破損recordは拒否する負caseのtest。

## Historical invariants

none

## Dependencies

none
