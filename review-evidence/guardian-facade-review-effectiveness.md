# Guardian審査と配備ルールの照合

採否はリポジトリ側の認可ルール変更を見送る。現行sourceと配備ルールに差はなく、調査した公開拒否は外部の承認判断が発生元だった。無人継続性の改善は未証明で、外部承認待ちは残る。

## 現行のルール

sourceは `1a63fcb4ba21c8daac932b86cde869c3309f54c9:codex/rules/glm-worker.rules`。現在配備されている `~/.codex/rules/glm-worker.rules` とバイト一致した。双方の SHA-256 は `7d8ef5e51088d5513c8497253b0ace4635f826f32db117f8eed280d7b2c54929`。実配備の `codex-cli 0.149.1` で、双方のファイルをそれぞれ `codex execpolicy check --rules` に渡した。

| 生 argv の種類 | source・配備の両方 |
| --- | --- |
| `glm-parent-action start`、`finalize-check go-test`、`install`、`controller-publication <token>` | `allow` |
| `/bin/sh -c 'glm-parent-action start && git push origin main'`、`/bin/sh -c 'glm-parent-action start; ./install.sh'` | このルールの一致なし |
| `./install.sh`、`git push origin main`、`git push --force origin main` | このルールの一致なし |

これは配備ルールの評価結果で、外部承認の結果は下記の実行記録を正とする。Facadeの引数解釈・staging・公開先の照合はリポジトリが所有する。Facadeのprefix allowと、外部審査が判断するGitHubへの公開権限は別の境界にある。

## 実 Bundle の審査と復帰

| Audit | 拒否から復帰まで | 審査の根拠と作用 |
| --- | --- | --- |
| [#1423](https://github.com/shinderuman/codex-worker-orchestrator/issues/1423) | 10月28日 18:35:44 UTCに `controller-publication` を拒否。18:36:49に親が確認し、18:37:09にユーザーが承認。18:37:18に同じ公開操作を許可し、公開完了。拒否からユーザー回答まで約85秒。 | 外部の判断は high risk / low authorization。審査対象の操作では書き込みを止めた。依頼から `origin/main` への具体的公開権限を認定できたかは未証明。 |
| [#1420](https://github.com/shinderuman/codex-worker-orchestrator/issues/1420) | 10月28日 16:07:48と16:08:33 UTCに公開を拒否。親はローカル検証を続け、16:34:31に承認を求めた。16:37:57に継続指示を受け、16:38:57に正規公開へ復帰。承認確認から継続指示まで約3分26秒。 | 外部の判断は双方 high risk / medium authorization。stale sourceによる別の停止と区別する。送信先・payloadへの明示承認不足と判定したことは確認済みで、誤拒否だったかは未証明。 |
| [#1399](https://github.com/shinderuman/codex-worker-orchestrator/issues/1399) | 10月26日 11:36 UTCに公開を拒否。11:36:59に親が確認し、12:30:12に承認。約53分13秒の人間待ちを経て正規作業を継続し、別途許可されたState復旧を経て完了。 | 公開の拒否は high risk / medium authorization。別の2件は、Taskメタデータの削除と本実装を含む広範な上書きを止めた。どちらも実際に作業を失い得るため認可境界を維持する。 |

3件とも追加のユーザー入力が発生し、その実行で無人継続性は未達だった。確認質問と公開再試行は確認できた最小の親側追加操作とする。通常作業全体に対する不要な親往復・tokenの総数は未分類。許可された全件を不要な審査には数えない。

| Audit | Guardian allow / deny / 不明 | 審査時間合計（ms） | 入力 / うちキャッシュ / 出力 |
| --- | ---: | ---: | ---: |
| #1423 | 36 / 1 / 0 | 89,239 | 3,274,976 / 2,986,496 / 4,810 |
| #1420 | 16 / 2 / 0 | 50,566 | 1,052,594 / 906,496 / 3,008 |
| #1399 | 182 / 3 / 1 | 475,909 | 21,591,280 / 18,807,680 / 23,034 |

集計は各 Bundle の `bound/transcripts/guardian/` にある完了turnを `turn_id` で重複排除した。時間は `task_complete.duration_ms`、tokenは各turnの最後の `last_token_usage`。審査時間と人間待ちは別集計。

- #1423 Bundle `12ae3721-8dbe-4ac9-8d40-a8490d28f17c`。Guardian `01a183ae-54b1-74d3-8570-a935d3b0a851` 392行。
- #1420 Bundle `a7e5e47e-e8ed-4ce4-9e58-0226578059a8`。Guardian `01a1831a-8078-7891-841f-da63b92059c3` 142・166行。
- #1399 Bundle `554d948b-7609-4412-bc93-55ce2ecaf5e2`。Guardian `01a177d3-41a3-7540-9810-62723883418d` 191行（公開）、434行（上書き）、`01a1747b-d00c-7273-af75-3357fdaabce0` 841行（メタデータ削除）。

本変更は実 CLI に対する回帰ケースだけを追加する。現行ルールで再現した不一致がないため、権限の拡張や公開手順の変更は行わない。前後の同条件rolloutは存在せず、削減量・Quality Delta・無人継続性の改善は未証明。
