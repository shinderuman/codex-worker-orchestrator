#!/usr/bin/env python3
"""Temporary live Codex prompt-capture probe for Issue #1649; remove after evidence is saved."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SOURCE = Path(os.environ["GITHUB_WORKSPACE"]).resolve()
GLOBAL_SENTINEL = "USER_OWNED_GLOBAL_SCOPE_SENTINEL"
PROJECT_SENTINEL = "UNRELATED_PROJECT_OWN_AGENTS_SENTINEL"
INSTALLED_ENTRYPOINT = "instructions/codex-worker-orchestrator.md"
BOOTSTRAP = "When glm-worker or glm-parent-action is used in this repository"

def command(args, cwd, env=None, timeout=90, must_pass=True):
    completed = subprocess.run(
        [str(item) for item in args], cwd=cwd, env=env,
        text=True, capture_output=True, timeout=timeout,
    )
    if must_pass and completed.returncode != 0:
        raise AssertionError(
            f"command {args} exited {completed.returncode}: "
            f"stderr={completed.stderr[-1500:]} stdout={completed.stdout[-900:]}"
        )
    return completed

def new_repo(path, with_plan):
    path.mkdir(parents=True)
    command(["git", "init", "-q", "-b", "main", str(path)], cwd=path)
    (path / "AGENTS.md").write_text("# " + PROJECT_SENTINEL + "\n")
    if with_plan:
        (path / "IMPLEMENTATION_PLAN.local.md").write_text("# nonharness plan\n")
    return path

class Capture(BaseHTTPRequestHandler):
    requests = []
    def log_message(self, *_args):
        return
    def do_GET(self):
        data = b'{"data":[]}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        payload = self.rfile.read(length)
        try:
            document = json.loads(payload)
        except ValueError:
            document = {"unparseable": payload[:100].decode(errors="replace")}
        self.requests.append((self.path, document))
        error = b'{"error":{"message":"scope probe intentionally stopped after capture","type":"invalid_request_error"}}'
        self.send_response(400)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(error)))
        self.end_headers()
        self.wfile.write(error)

def main():
    with tempfile.TemporaryDirectory(prefix="codex-runtime-scope-") as location:
        root = Path(location).resolve()
        codex_home = root / "codex-home"
        codex_home.mkdir()
        original_agents = "# " + GLOBAL_SENTINEL + "\n"
        (codex_home / "AGENTS.md").write_text(original_agents)
        binaries = root / "bin"
        binaries.mkdir()
        command(["go", "build", "-o", binaries / "codex-install", "./cmd/codex-install"],
                cwd=SOURCE / "glm-worker", timeout=180)
        command(["go", "build", "-o", binaries / "glm-codex-context", "./cmd/glm-codex-context"],
                cwd=SOURCE / "glm-worker", timeout=180)
        command([binaries / "codex-install", "--repo-root", SOURCE, "--codex-dir", codex_home],
                cwd=SOURCE)
        assert (codex_home / "AGENTS.md").read_text() == original_agents, "installer modified user AGENTS"
        assert (codex_home / INSTALLED_ENTRYPOINT).read_bytes() == (SOURCE / "codex/AGENTS.md").read_bytes()

        server = ThreadingHTTPServer(("127.0.0.1", 0), Capture)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            repositories = [
                ("unrelated-no-plan", new_repo(root / "unrelated-no-plan", False), False),
                ("unrelated-with-plan", new_repo(root / "unrelated-with-plan", True), False),
                ("opt-in-no-plan", new_repo(root / "opt-in-no-plan", False), True),
                ("opt-in-with-plan", new_repo(root / "opt-in-with-plan", True), True),
                ("orchestrator-source", SOURCE, False),
            ]
            config = codex_home / "config.toml"
            provider = (
                '\nmodel = "gpt-5.2-codex"\n'
                'model_provider = "scope-probe"\n'
                '[model_providers.scope-probe]\n'
                'name = "Scope Probe"\n'
                'base_url = "http://127.0.0.1:' + str(server.server_port) + '/v1"\n'
                'env_key = "SCOPE_PROBE_API_KEY"\n'
                'wire_api = "responses"\n'
            )
            for _name, repo, _enabled in repositories:
                provider += '\n[projects.' + json.dumps(str(repo)) + ']\ntrust_level = "trusted"\n'
            with config.open("a") as out:
                out.write(provider)

            env = dict(os.environ)
            env.update(CODEX_HOME=str(codex_home), CODEX_CONFIG_DIR=str(codex_home),
                       SCOPE_PROBE_API_KEY="test-only-fake-token", CI="true", NO_COLOR="1")
            cli_version = command(["codex", "--version"], cwd=SOURCE).stdout.strip()
            print("codex_runtime_version=" + cli_version, flush=True)
            for name, repo, enabled in repositories:
                if enabled:
                    command([binaries / "glm-codex-context", "enable", repo], cwd=SOURCE)
                assert (codex_home / "AGENTS.md").read_text() == original_agents
                local_agents = (repo / "AGENTS.md").read_bytes()
                Capture.requests.clear()
                result = command(
                    ["codex", "exec", "--skip-git-repo-check", "--json", "-C", repo,
                     "Reply scope probe, do not call tools."],
                    cwd=repo, env=env, timeout=60, must_pass=False,
                )
                requests = [document for path, document in Capture.requests
                            if path.rstrip("/").endswith("/responses")]
                if not requests:
                    raise AssertionError(
                        f"{name}: no actual Codex Responses request was captured; "
                        f"exit={result.returncode}; stderr={result.stderr[-1800:]}; "
                        f"stdout={result.stdout[-1000:]}; endpoints={[path for path, _ in Capture.requests]}"
                    )
                serialized = json.dumps(requests[0], ensure_ascii=False)
                actual_bootstrap = BOOTSTRAP in serialized and INSTALLED_ENTRYPOINT in serialized
                expected_bootstrap = enabled
                if actual_bootstrap != expected_bootstrap:
                    raise AssertionError(
                        f"{name}: expected installed instruction bootstrap={expected_bootstrap}, "
                        f"actual={actual_bootstrap}; request keys={list(requests[0].keys())}; "
                        f"excerpt={serialized[-1800:]}"
                    )
                if not enabled and INSTALLED_ENTRYPOINT in serialized:
                    raise AssertionError(f"{name}: namespaced instructions leaked without opt-in")
                if not enabled and "# Codex グローバルルール" in serialized:
                    raise AssertionError(f"{name}: installed instruction body leaked without opt-in")
                if name.startswith("unrelated") or enabled:
                    if PROJECT_SENTINEL not in serialized:
                        raise AssertionError(f"{name}: user's repository AGENTS was not applied")
                if name == "orchestrator-source" and "Repository bootstrap" not in serialized:
                    raise AssertionError("orchestrator source project bootstrap absent")
                if (repo / "AGENTS.md").read_bytes() != local_agents:
                    raise AssertionError(f"{name}: repository AGENTS changed")
                print(
                    f"PROBE PASS {name} opt_in={enabled} "
                    f"bootstrap_sent={actual_bootstrap} requests={len(requests)} "
                    f"global_agents_preserved=true", flush=True
                )
            assert (codex_home / "AGENTS.md").read_text() == original_agents
        finally:
            server.shutdown()
            server.server_close()

if __name__ == "__main__":
    main()
