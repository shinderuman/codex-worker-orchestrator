# Task: 不正Worker/Reviewer Packetの有限補正と正規再入

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「最終的なFindingはCodexに実装させようと思う」。

## Resolved references

Worker NEEDS_SOL_DECISIONのschema不一致、Reviewer TARGETS/proof/byte budgetと再提出の実停止（#1347 J04/J05、#1356 F03/F04、#1359 G02/G03、#1377 R04、#1382 T10）。今回#1392でもReviewer invalid_packetが2件。

- PR #1368 / comment 4326838969: 有効reviewer PASSをJSON全文付きIssuesへ埋め、2048byte field上限を超え得る。MachineJSONはserializeのみでwrapper生成packetをvalidateしない。rawは既存artifactに保持し、boundedな表示とsource locatorを使用する。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/workflow/workflow.go:riskFloorEscalationResult`
- `glm-worker/internal/packet/result.go:MachineJSON`
- `glm-worker/internal/packet/validate.go:validateFields`

- PR #1368 / comment 4326838949: v6のjson.Unmarshalとpersisted-key拒否リストはrisk_floor_reemitを拒否しない。旧flagを黙殺せずcurrent-schema不正として拒否する範囲だけ採用し、migrationやre-emissionを復活させない。

- `glm-worker/internal/state/resume.go:LoadResumeCheckpoint`
- `glm-worker/internal/state/resume.go:validateResumePersistedKeys`
- `glm-worker/internal/workflow/resume_flow.go:resolveResumedReviewResult`

## Amendments

### 2026-10-25 External Reviewから同一責務へ統合: risk-floor-review-packet-bounds

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
有効なreviewer PASSを昇格しても結果packetのfield/全体上限を守る。
raw reviewer JSONをIssuesへ重複埋込せず、元packetは既存artifact/recordのexact locatorで保持する。Solが必要な原文を回収できるようにする。
wrapper生成のNEEDS_SOL_REVIEWにも既存packet制約を適用し、2048byte fieldと8KiB packetを超えて無検証emitしない。
HIGH risk-floor・targets・coverageの不確実性を落とさず、無言truncateで意味を削らない。
````

### 2026-10-25 External Reviewから同一責務へ統合: resume-obsolete-risk-floor-key-rejection

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
risk_floor_reemitを持つ旧入力を現行v6として黙殺しない。
current checkpoint loaderで廃止risk_floor_reemit keyを検出・拒否し、通常review resumeへ縮退させない。
旧再送処理・migration・aliasを復活せず、有効なcurrent checkpointのHIGH risk-floorを維持する。
````

## Purpose

既存Packet schema・モデル出力契約・Task/Attempt/Review sessionを照合し、修正可能な違反のみ限定feedbackで再送させ、必要なSol判断・独立Reviewへ正常復帰する。

## External feasibility

status: not-applicable

## Contract

既存Packet schema・モデル出力契約・Task/Attempt/Review sessionを照合し、修正可能な違反のみ限定feedbackで再送させ、必要なSol判断・独立Reviewへ正常復帰する。既存 resubmit-report / correction ownerの実効性を先に検証する。

External Reviewで確認した同一責務の具体条件（risk-floor-review-packet-bounds）:

- raw reviewer JSONをIssuesへ重複埋込せず、元packetは既存artifact/recordのexact locatorで保持する。Solが必要な原文を回収できるようにする。
- wrapper生成のNEEDS_SOL_REVIEWにも既存packet制約を適用し、2048byte fieldと8KiB packetを超えて無検証emitしない。
- HIGH risk-floor・targets・coverageの不確実性を落とさず、無言truncateで意味を削らない。

External Reviewで確認した同一責務の具体条件（resume-obsolete-risk-floor-key-rejection）:

- current checkpoint loaderで廃止risk_floor_reemit keyを検出・拒否し、通常review resumeへ縮退させない。
- 旧再送処理・migration・aliasを復活せず、有効なcurrent checkpointのHIGH risk-floorを維持する。

## Must not

不正packetをPASSへ格上げしない。Schema互換や自由形式JSONでGuardを迂回しない。独立Reviewのsemantic指摘を削らない。

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

非法TARGETS/size/decision packet→有限corrected packet→独立Review/最終Gateへの正常復帰を現行production入口で再現する。旧source Reviewの再利用は拒否し、無意味な追加モデルcallを測定・低減する。

- 上限に近い有効PASSの通常/再開経路でboundedなNEEDS_SOL_REVIEWを生成する。
- Solが元reviewer結果をexact locatorから確認でき、raw保持と表示制約が両立する。
- HIGHをLOW/PASSへ縮退せず、過大・不正packetは所定のfail-closedになる。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

- risk_floor_reemitを含むv6入力はmodel call/state変更前に拒否する。
- 正当なcurrent review checkpointの再開とHIGH floorが維持される。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
