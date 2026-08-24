// glm-workerはGLM Coding Plan上でSol Highと協調する永続実装ワーカーCLI。
// 本ファイルは薄いentrypointであり、実装は internal 配下のpackageへ委譲する。
package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		// 失敗はstderrのJSON 1行とnon-zero exitで示す。stderrへの書込み自体が失敗した
		// 時候補の出力先がないため、exit codeだけが失敗の通知になる。
		_ = app.WriteProcessError(os.Stderr, err)
		os.Exit(1)
	}
}
