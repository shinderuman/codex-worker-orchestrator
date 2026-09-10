from pathlib import Path

wf = Path('glm-worker/internal/workflow/workflow.go')
s = wf.read_text()
start = 'func (w *Workflow) runModel('
end = 'func boundedText('
a = s.index(start)
b = s.index(end, a)
block = s[a:b].rstrip() + '\n'
s = s[:a] + s[b:]
Path('glm-worker/internal/workflow/model_call.go').write_text('''package workflow

import (
\t"crypto/sha256"
\t"encoding/hex"
\t"errors"
\t"fmt"
\t"path/filepath"
\t"strings"
\t"time"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

''' + block)
wf.write_text(s)
