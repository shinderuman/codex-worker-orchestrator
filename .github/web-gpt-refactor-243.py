from pathlib import Path

path = Path("glm-worker/internal/app/bundle.go")
text = path.read_text()
old = '''func currentBundleTask(st *state.StateStore, taskID string) bundleTask {
\tstats, _ := st.CurrentTaskStats()
\tstatus := string(st.TaskStatus())
\tif status == string(state.TaskStatusNone) && stats.TaskID == taskID {
\t\tstatus = string(stats.Status)
\t}
\treturn bundleTask{ID: taskID, Status: status, Current: true, Stats: stats}
}
'''
new = '''func currentBundleTask(st *state.StateStore, taskID string) bundleTask {
\tstats, _ := st.CurrentTaskStats()
\treturn bundleTask{ID: taskID, Status: string(st.TaskStatus()), Current: true, Stats: stats}
}
'''
if old not in text:
    raise SystemExit("currentBundleTask replacement target missing")
path.write_text(text.replace(old, new, 1))
