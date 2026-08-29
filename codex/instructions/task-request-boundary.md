# ACTIVE task要求とrun-controlの分離

ACTIVE taskを開始・再開するとき、user messageをdurable task contractへ反映する境界にだけ適用する。

## Durable requirement

- `## Amendments`へ永続化するのは、task固有の要求・制約・Acceptance criteria・既存Sol判断の意味を変更するsemantic deltaだけとする。
- semantic deltaは省略・要約で意味を落とさず、次のmodel call前にtask fileへ反映する。decision/fix本文だけでdurable requirementを差し替えない。
- 既にACTIVE task fileへ置いたsubstantive requirementを`USER_REQUEST`へ重複転記しない。`USER_REQUEST`はtask要旨・task file参照と、その呼出に必要なrun-controlだけに留める。

## Run control

- start/resume許可、現在task後の停止境界、runtime preflight、待機・進捗報告、親Codexのreview/commit/Plan・History更新、最終報告形式は親orchestrationの実行状態であり、userがdurable task contractへ含めると明示した場合を除き`## Amendments`へコピーしない。
- run-controlだけのmessageではtask fileを変更しない。
- 1つのmessageにsemantic amendmentとrun-controlが混在する場合、semantic amendmentだけをlosslessに分離してtask fileへ反映し、message全体をamendmentとして転記しない。
- user messageの意味を判定するためのgeneric classifier・追加model callは作らない。親Codexが通常の要求解釈として境界を判断する。

## 境界例

- `ACTIVE taskを開始し、完了したら次taskへ進まず結果を報告する`はrun-controlだけなのでtask fileを変更しない。
- `ACTIVE taskを開始する。production test selectionは導入しない、をMust notへ追加する`は後半だけをdurable requirementとしてtask fileへ反映し、開始・停止・報告手順は転記しない。
