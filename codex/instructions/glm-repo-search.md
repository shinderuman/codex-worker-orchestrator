# repo-search featureの制御

`glm-worker`のrepo-searchは、semantic questionから確認対象を絞るためのbounded navigation supportである。`GLM_WORKER_REPO_SEARCH`の解釈、CLI parser/read-only execution、bounded result/refinement、parent-evidence bookkeepingはcurrent production codeとtestを実装契約の正とする。

## semantic boundary

- BM25の上位結果はnavigation locatorであり、source-code proofではない。結論へ使う前に対象の現物を確認する。
- 親は調べたい意味、question、必要なscopeとbudgetを判断する。exact CLI/output、feature flagのdefault/disabled/refinement/duplicate処理はruntimeへ委ね、同じprocedureを自由言語で再構築しない。
- optionalなBM25 navigationとquality-requiredなexhaustive proofを混同しない。task contractがexhaustive evidenceを明示的に要求した場合のactivationとfull-corpus proofは`control:repo-search-exhaustive-activation`が機械強制し、BM25 top-Nだけをexhaustive evidenceとして扱わない。`GLM_WORKER_REPO_SEARCH`の状態をrequired proofの免除理由にしない。親は検索predicateが要求された意味を十分表すかを判断する。
- generic repo-search engine / read-only CLIと、repository task contractからexhaustive proofをactivateするworkflow wiringは別authorityである。一方の詳細を他方のproseへ複製しない。
