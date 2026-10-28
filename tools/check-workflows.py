#!/usr/bin/env python3
"""check-workflows.py: GitHub Actions hygiene that a repo taking fork PRs needs.

Fails on:
  - a `runs-on` that isn't a GitHub-hosted label (ubuntu-*, macos-*, windows-*). Lists,
    block lists and `${{ matrix.X }}` are resolved; a runner group or any other expression is
    refused, never guessed;
  - a `pull_request_target` trigger, in any `on:` form;
  - a `uses:` not pinned to a full 40-hex commit SHA (local `./` actions are fine; docker
    images need an `@sha256:` digest).

Usage: check-workflows.py [workflow files...]   (default: .github/workflows/*.yml|*.yaml)
Exit 0 = clean, 1 = findings (printed as file:line: message), 2 = usage.

Stdlib only and self-contained, so other repos can vendor this one file (pin it by sha256).
It reads workflows line by line rather than as full YAML; constructs it can't follow are
reported, not skipped.
"""
import pathlib
import re
import sys

HOSTED = re.compile(r"^(ubuntu|macos|windows)-[A-Za-z0-9.-]+$")
PINNED = re.compile(r"^[^@\s]+@[0-9a-f]{40}$")
DIGEST = re.compile(r"^docker://[^@\s]+@sha256:[0-9a-f]{64}$")
MATRIX_REF = re.compile(r"^\$\{\{\s*matrix\.([A-Za-z0-9_-]+)\s*\}\}$")


def strip_comment(line):
    """Drop a trailing # comment (outside quotes)."""
    out, quote = [], None
    for i, ch in enumerate(line):
        if quote:
            if ch == quote:
                quote = None
        elif ch in "'\"":
            quote = ch
        elif ch == "#" and (i == 0 or line[i - 1].isspace()):
            break
        out.append(ch)
    return "".join(out).rstrip()


def unquote(v):
    v = v.strip()
    if len(v) >= 2 and v[0] == v[-1] and v[0] in "'\"":
        return v[1:-1]
    return v


def indent(line):
    return len(line) - len(line.lstrip(" "))


def flow_list(v):
    return [unquote(x) for x in v.strip()[1:-1].split(",") if x.strip()]


def block(lines, i):
    """Lines (index, text) indented deeper than lines[i], until the indentation returns."""
    base, out = indent(lines[i]), []
    for j in range(i + 1, len(lines)):
        if not lines[j].strip():
            continue
        if indent(lines[j]) <= base:
            break
        out.append((j, lines[j]))
    return out


def matrix_values(lines):
    """{key: [values]} from every matrix: block in the file, including include: entries."""
    vals = {}
    for i, line in enumerate(lines):
        if not re.match(r"^\s*matrix:\s*$", line):
            continue
        body = block(lines, i)
        for k, (j, text) in enumerate(body):
            m = re.match(r"^\s*(?:-\s+)?([A-Za-z0-9_-]+):\s*(.*)$", text)
            if not m or m.group(1) in ("include", "exclude"):
                continue
            key, v = m.group(1), m.group(2).strip()
            if v.startswith("["):
                vals.setdefault(key, []).extend(flow_list(v))
            elif v:
                vals.setdefault(key, []).append(unquote(v))
            else:
                for _, item in block(lines, j):
                    im = re.match(r"^\s*-\s+(.+)$", item)
                    if im:
                        vals.setdefault(key, []).append(unquote(im.group(1)))
    return vals


def check_label(label, matrix, report):
    m = MATRIX_REF.match(label)
    if m:
        values = matrix.get(m.group(1))
        if not values:
            report(f"runs-on {label}: can't resolve matrix.{m.group(1)}; list its values in strategy.matrix")
            return
        for v in values:
            check_label(v, {}, report)
        return
    if "${{" in label:
        report(f"runs-on {label}: can't resolve this expression; use a GitHub-hosted label")
    elif not HOSTED.match(label):
        report(f"runs-on {label}: not a GitHub-hosted label (ubuntu-*, macos-*, windows-*); self-hosted runners are not allowed")


def check(path):
    findings = []
    lines = [strip_comment(l) for l in path.read_text(encoding="utf-8").splitlines()]
    matrix = matrix_values(lines)

    for i, line in enumerate(lines):
        def report(msg, n=i):
            findings.append(f"{path}:{n + 1}: {msg}")

        # Triggers: the top-level on: key, inline or as a block.
        m = re.match(r"^[\"']?on[\"']?:\s*(.*)$", line)
        if m:
            region = [m.group(1)] + [t for _, t in block(lines, i)]
            if any(re.search(r"(^|[\s\[,'\"-])pull_request_target([\s\]:,'\"]|$)", t) for t in region):
                report("pull_request_target runs fork code with this repo's secrets; use pull_request")

        m = re.match(r"^\s*(?:-\s+)?uses:\s*(.+)$", line)
        if m:
            ref = unquote(m.group(1))
            if ref.startswith("./"):
                pass
            elif ref.startswith("docker://"):
                if not DIGEST.match(ref):
                    report(f"uses {ref}: pin the image by digest (@sha256:<64 hex>)")
            elif not PINNED.match(ref):
                report(f"uses {ref}: not pinned to a full commit SHA")

        m = re.match(r"^\s*runs-on:\s*(.*)$", line)
        if m:
            v = m.group(1).strip()
            if v.startswith("{"):
                report("runs-on uses a runner group or mapping; only GitHub-hosted labels are allowed")
            elif v.startswith("["):
                for label in flow_list(v):
                    check_label(label, matrix, report)
            elif v:
                check_label(unquote(v), matrix, report)
            else:
                body = block(lines, i)
                if not body:
                    report("runs-on is empty")
                for j, text in body:
                    im = re.match(r"^\s*-\s+(.+)$", text)
                    if im:
                        check_label(unquote(im.group(1)), matrix, lambda msg, n=j: findings.append(f"{path}:{n + 1}: {msg}"))
                    else:
                        report("runs-on uses a runner group or mapping; only GitHub-hosted labels are allowed")
                        break
    return findings


def main(argv):
    if any(a in ("-h", "--help") for a in argv):
        print(__doc__.strip())
        return 0
    paths = [pathlib.Path(a) for a in argv] or sorted(
        p for p in pathlib.Path(".github/workflows").glob("*") if p.suffix in (".yml", ".yaml"))
    if not paths:
        print("check-workflows: no workflow files found (pass paths, or run at the repo root)", file=sys.stderr)
        return 2
    findings = []
    for p in paths:
        try:
            findings += check(p)
        except OSError as e:
            print(f"check-workflows: cannot read {p} ({e.__class__.__name__})", file=sys.stderr)
            return 2
    for f in findings:
        print(f)
    print(f"check-workflows: {len(paths)} file(s), {len(findings)} finding(s)")
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
