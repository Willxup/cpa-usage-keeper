#!/usr/bin/env python3
"""在隔离的数据库副本上比较旧、新 Keeper 的实际长范围 HTTP 查询。"""

import argparse
import datetime as dt
import http.server
import json
import os
from pathlib import Path
import socket
import sqlite3
import statistics
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request


class SyntheticCPA(http.server.BaseHTTPRequestHandler):
    """只返回失败，防止测试时访问真实 CPA 或用空成功结果覆盖已有身份。"""

    def do_GET(self):
        self.send_response(404)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b"{}")

    def log_message(self, *_):
        pass


def clone_database(source, target):
    """使用 SQLite 快照生成全新副本，原备份及已升级数据库只读。"""
    target.parent.mkdir(parents=True)
    with sqlite3.connect(source.resolve().as_uri() + "?mode=ro", uri=True) as src:
        with sqlite3.connect(target) as dst:
            src.backup(dst)


def free_port():
    """为独立测试实例选择本机空闲端口，不使用对外监听地址。"""
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def get_json(url, timeout=120):
    """读取真实 API；网络及业务错误交调用者处理，不把失败样本当快速查询。"""
    request = urllib.request.Request(url, headers={"X-CPA-Usage-Keeper-Request": "fetch"})
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.load(response)


def wait_ready(process, base, current):
    """旧版等监听就绪，新版还要等迁移 ready；healthz 的 200 不代表业务可读。"""
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"Keeper exited before ready: {process.returncode}")
        try:
            if current:
                status = get_json(base + "/api/v1/startup/status", 1)
                if status["phase"] == "failed":
                    raise RuntimeError("current Keeper startup failed; inspect its test log")
                if status["phase"] == "ready":
                    return
            else:
                with urllib.request.urlopen(base + "/healthz", timeout=1) as response:
                    if response.status == 200:
                        return
        except (urllib.error.URLError, TimeoutError):
            pass
        time.sleep(0.1)
    raise RuntimeError("Keeper did not become ready within 120 seconds")


def query_facts(body, analysis):
    """只比较共同的请求和 Token 事实；两版金额公式不同，不用金额相等伪装等价。"""
    if analysis:
        buckets = body["token_usage"]
        return {
            "requests": sum(row["requests"] for row in buckets),
            "tokens": sum(row["total_tokens"] for row in buckets),
        }
    return {"requests": body["usage"]["total_requests"], "tokens": body["usage"]["total_tokens"]}


def measure(binary, database, directory, cpa_url, suffix, samples, current):
    """顺序启动单个真实实例，预热后测 HTTP 延迟；无论成功失败均回收该子进程。"""
    work = directory / "data"
    clone_database(database, work / "app.db")
    port = free_port()
    env = {
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"), "TZ": "UTC",
        "APP_HOST": "127.0.0.1", "APP_PORT": str(port), "WORK_DIR": str(work),
        "AUTH_ENABLED": "false", "CPA_BASE_URL": cpa_url, "CPA_MANAGEMENT_KEY": "synthetic-only",
        "BACKUP_ENABLED": "false", "LOG_FILE_ENABLED": "false", "LOG_LEVEL": "error",
        "REQUEST_TIMEOUT": "1s", "REDIS_QUEUE_IDLE_INTERVAL": "1h",
        "GOMEMLIMIT": "5GiB",
    }
    result = {}
    with (directory / "keeper.log").open("wb") as log:
        process = subprocess.Popen([str(binary.resolve())], cwd=directory, env=env, stdout=log, stderr=log)
        try:
            base = f"http://127.0.0.1:{port}"
            wait_ready(process, base, current)
            for endpoint in ("overview", "analysis"):
                url = base + "/api/v1/usage/" + endpoint + suffix
                expected = query_facts(get_json(url), endpoint == "analysis")
                if expected["requests"] <= 0 or expected["tokens"] <= 0:
                    raise RuntimeError(f"{endpoint} did not cover the intended synthetic history")
                elapsed = []
                for _ in range(samples):
                    started = time.perf_counter()
                    body = get_json(url)
                    elapsed.append((time.perf_counter() - started) * 1000)
                    if query_facts(body, endpoint == "analysis") != expected:
                        raise RuntimeError(f"{endpoint} facts changed between samples")
                result[endpoint] = {
                    "facts": expected, "samples_ms": elapsed, "median_ms": statistics.median(elapsed),
                    "min_ms": min(elapsed), "max_ms": max(elapsed),
                }
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
    return result


def main():
    """比较同一合成历史的两个 schema 副本，输出可复现参数与原始延迟样本。"""
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("old-binary", "new-binary", "backup", "database", "root"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--anchor", required=True, help="UTC pricingbench anchor")
    parser.add_argument("--samples", type=int, default=5)
    args = parser.parse_args()
    if args.samples < 1:
        parser.error("samples must be positive")
    anchor = dt.datetime.fromisoformat(args.anchor.replace("Z", "+00:00"))
    if anchor.tzinfo is None or anchor.utcoffset() != dt.timedelta(0) or anchor.time() != dt.time():
        parser.error("comparison anchor must be UTC midnight")
    args.root = args.root.resolve()
    args.root.mkdir(parents=True, exist_ok=False)
    # 注入消息时间是 anchor 前十分钟；排除它所在的最后一天，只比较两库共同的 119 天历史。
    query = {"range": "custom", "unit": "day", "start": (anchor - dt.timedelta(days=120)).date().isoformat(),
             "end": (anchor - dt.timedelta(days=2)).date().isoformat()}
    suffix = "?" + urllib.parse.urlencode(query)
    cpa = http.server.ThreadingHTTPServer(("127.0.0.1", 0), SyntheticCPA)
    thread = threading.Thread(target=cpa.serve_forever, daemon=True)
    thread.start()
    try:
        cpa_url = f"http://127.0.0.1:{cpa.server_port}"
        old = measure(args.old_binary, args.backup, args.root / "old", cpa_url, suffix, args.samples, False)
        new = measure(args.new_binary, args.database, args.root / "new", cpa_url, suffix, args.samples, True)
        for endpoint in ("overview", "analysis"):
            if old[endpoint]["facts"] != new[endpoint]["facts"]:
                raise RuntimeError(f"old/new {endpoint} compared different request or Token sets")
        result = {"anchor": args.anchor, "query": query, "range_days": 119, "warmup_per_endpoint": 1,
                  "samples_per_endpoint": args.samples, "old": old, "new": new}
        output = args.root / "query-comparison.json"
        output.write_text(json.dumps(result, indent=2) + "\n")
        print(output)
    finally:
        cpa.shutdown()
        cpa.server_close()
        thread.join(timeout=5)


if __name__ == "__main__":
    main()
