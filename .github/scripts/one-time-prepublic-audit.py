#!/usr/bin/env python3
"""Ephemeral pre-public audit: redacted aggregate output only; never upload raw findings."""
import collections
import io
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
import zipfile

repo = "convention-x-treme/lansurfer"
token = os.getenv("GH_TOKEN", "")
temp = os.getenv("RUNNER_TEMP", "/tmp")
max_zip = 40 * 1024 * 1024
max_text = 15 * 1024 * 1024
patterns = {
    "private-key-marker": rb"-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----",
    "github-token": rb"(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})",
    "discord-webhook": rb"https://discord(?:app)?\.com/api/webhooks/[0-9]{8,}/[A-Za-z0-9_-]{20,}",
    "aws-access-key": rb"(?:AKIA|ASIA)[A-Z0-9]{16}",
    "credential-assignment": rb"(?i)(?:client_secret|webhook_url|vapid_private_key|api[_-]?secret|db_password)[ \"']*[:=][ \"']*[A-Za-z0-9_./+=-]{16,}",
}
compiled = {key: re.compile(value) for key, value in patterns.items()}
counts = collections.Counter()
result = {
    "git": {},
    "actions": {"runs": 0, "logs_scanned": 0, "logs_skipped": 0,
                "artifacts": 0, "artifacts_scanned": 0, "artifacts_skipped": 0,
                "text_entries_scanned": 0},
}


def api(path, binary=False):
    url = "https://api.github.com/repos/" + repo + path
    request = urllib.request.Request(url, headers={
        "Authorization": "Bearer " + token,
        "Accept": "application/vnd.github+json",
        "User-Agent": "LANsurfer-audit-once",
    })
    with urllib.request.urlopen(request, timeout=30) as response:
        body = response.read(max_zip + 1 if binary else 15 * 1024 * 1024)
    if binary:
        return body if len(body) <= max_zip else None
    return json.loads(body)


def pages(endpoint, key):
    for page in range(1, 31):
        separator = "&" if "?" in endpoint else "?"
        batch = api(endpoint + separator + "per_page=100&page=" + str(page))[key]
        yield from batch
        if len(batch) < 100:
            break


def scan_zip(body):
    if not body:
        return False
    try:
        with zipfile.ZipFile(io.BytesIO(body)) as z:
            for entry in z.infolist():
                if entry.is_dir() or entry.file_size > max_text:
                    continue
                with z.open(entry) as stream:
                    content = stream.read(max_text + 1)
                if b"\x00" in content[:2048]:
                    continue
                result["actions"]["text_entries_scanned"] += 1
                for name, regex in compiled.items():
                    counts[name] += len(regex.findall(content))
        return True
    except (OSError, ValueError, RuntimeError, zipfile.BadZipFile):
        return False


def git_scan(mode):
    report = temp + "/gitleaks-" + mode + ".json"
    command = [temp + "/bin/gitleaks", mode, "--source=.", "--no-banner",
               "--redact=100", "--log-level=error", "--report-path=" + report]
    if mode == "git":
        command.append("--log-opts=--all")
    completed = subprocess.run(command, stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, check=False)
    try:
        with open(report, encoding="utf-8") as file:
            findings = json.load(file)
    except (FileNotFoundError, ValueError):
        findings = []
    grouped = collections.Counter(str(finding.get("RuleID", "unknown")) for finding in findings)
    return {"exit_code": completed.returncode, "findings": len(findings),
            "rules": dict(grouped)}


def main():
    result["git"]["commits"] = int(subprocess.check_output(
        ["git", "rev-list", "--all", "--count"], text=True).strip())
    result["git"]["refs"] = len(subprocess.check_output(
        ["git", "for-each-ref", "--format=%(refname)"], text=True).splitlines())
    result["git"]["history"] = git_scan("git")
    result["git"]["working_tree"] = git_scan("dir")
    if token:
        try:
            for run in pages("/actions/runs", "workflow_runs"):
                result["actions"]["runs"] += 1
                if str(run["id"]) == os.getenv("GITHUB_RUN_ID"):
                    continue
                try:
                    ok = scan_zip(api("/actions/runs/" + str(run["id"]) + "/logs", True))
                except (urllib.error.URLError, TimeoutError, OSError):
                    ok = False
                result["actions"]["logs_scanned" if ok else "logs_skipped"] += 1
            for item in pages("/actions/artifacts", "artifacts"):
                result["actions"]["artifacts"] += 1
                if item.get("expired") or item.get("size_in_bytes", 0) > max_zip:
                    result["actions"]["artifacts_skipped"] += 1
                    continue
                try:
                    ok = scan_zip(api("/actions/artifacts/" + str(item["id"]) + "/zip", True))
                except (urllib.error.URLError, TimeoutError, OSError):
                    ok = False
                result["actions"]["artifacts_scanned" if ok else "artifacts_skipped"] += 1
        except (urllib.error.URLError, TimeoutError, ValueError, OSError) as error:
            result["actions"]["api_error_type"] = type(error).__name__
    else:
        result["actions"]["api_error_type"] = "MissingToken"
    result["actions"]["pattern_counts"] = dict(counts)
    result["actions"]["complete"] = (
        "api_error_type" not in result["actions"]
        and result["actions"]["logs_skipped"] == 0
        and result["actions"]["artifacts_skipped"] == 0
    )
    print("SANITIZED_PREPUBLIC_AUDIT=" + json.dumps(result, sort_keys=True))
    # Raw Gitleaks reports and downloaded logs are never uploaded, printed or committed.
    return 0 if (result["git"]["history"]["exit_code"] in (0, 1)
                 and result["git"]["working_tree"]["exit_code"] in (0, 1)) else 2


if __name__ == "__main__":
    sys.exit(main())
