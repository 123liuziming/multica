#!/usr/bin/env python3
"""One-shot Aone→Multica issue sync. Runs on the host (where a1 CLI is available)."""

import json
import subprocess
import sys
import urllib.request
import uuid
import hashlib

MULTICA_API = "http://100.69.248.97:18080"
TOKEN = "mul_21ec903329fa6e5cace283b0883a3bf54beb1c6c"
WORKSPACE_ID = sys.argv[1] if len(sys.argv) > 1 else "d60b4fd2-9513-4e3d-95ab-70e11d787c99"
AONE_PROJECT_ID = sys.argv[2] if len(sys.argv) > 2 else "1055266"
AONE_NS = uuid.UUID(WORKSPACE_ID)

STATUS_MAP = {
    "open": "todo", "reopen": "todo", "new": "todo",
    "新建": "todo", "重新打开": "todo", "待处理": "todo",
    "in progress": "in_progress", "开发中": "in_progress", "实现中": "in_progress",
    "done": "done", "closed": "done", "已完成": "done", "已关闭": "done", "已发布": "done",
    "cancelled": "cancelled", "已取消": "cancelled", "废弃": "cancelled",
    "in review": "in_review", "评审中": "in_review", "测试中": "in_review",
    "fixed": "done", "invalid": "cancelled",
}

PRIORITY_MAP = {
    "urgent": "urgent", "紧急": "urgent",
    "high": "high", "高": "high",
    "medium": "medium", "中": "medium",
    "low": "low", "低": "low",
}


def derive_dedup_uuid(ws_id: str, aone_id: str) -> str:
    ns = uuid.UUID(ws_id)
    return str(uuid.uuid5(ns, f"aone:{aone_id}"))


def map_status(s: str) -> str:
    return STATUS_MAP.get(s.lower().strip(), "backlog")


def map_priority(p: str) -> str:
    return PRIORITY_MAP.get(p.lower().strip(), "none")


def api_request(method, path, body=None):
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(
        f"{MULTICA_API}{path}",
        data=data,
        method=method,
        headers={
            "Authorization": f"Bearer {TOKEN}",
            "Content-Type": "application/json",
            "X-Workspace-ID": WORKSPACE_ID,
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read()), resp.status
    except urllib.error.HTTPError as e:
        return json.loads(e.read()), e.code


def fetch_aone_items():
    result = subprocess.run(
        ["a1", "project", "workitem", "list",
         "--project", AONE_PROJECT_ID,
         "--format", "json",
         "--columns", "id,subject,status,priority,categoryIdentifier,gmtCreate"],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    if result.returncode != 0:
        print("a1 failed: {}".format(result.stderr.decode()), file=sys.stderr)
        sys.exit(1)
    return json.loads(result.stdout.decode())


def main():
    print(f"Syncing Aone project {AONE_PROJECT_ID} → workspace {WORKSPACE_ID}")
    items = fetch_aone_items()
    print(f"Fetched {len(items)} work items from Aone")

    created = 0
    skipped = 0
    failed = 0

    for item in items:
        aone_id = str(item["identifier"])
        title = item["subject"]
        status = map_status(item.get("status", ""))
        priority = map_priority(item.get("priority", ""))
        category = item.get("categoryIdentifier", "")
        origin_id = derive_dedup_uuid(WORKSPACE_ID, aone_id)

        body = {
            "title": title,
            "description": f"[Aone {category} #{aone_id}]",
            "status": status,
            "priority": priority,
            "origin_type": "aone",
            "origin_id": origin_id,
        }

        resp, code = api_request("POST", "/api/issues", body)

        if code == 201:
            created += 1
            print(f"  + [{aone_id}] {title}")
        elif code == 409 or (code == 400 and "origin" in json.dumps(resp).lower()):
            skipped += 1
            print(f"  = [{aone_id}] {title} (already synced)")
        else:
            failed += 1
            print(f"  ! [{aone_id}] {title} (error {code}: {resp})")

    print(f"\nDone: created={created}, skipped={skipped}, failed={failed}")


if __name__ == "__main__":
    main()
