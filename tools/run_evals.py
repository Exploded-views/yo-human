#!/usr/bin/env python3
"""run_evals.py: run the yo-human skill evals once per arm, then grade the replies.

Adapted from crewkit@5734067a2277463213b8206a6f2b1afd3ecdbed9 evals/run_cases.py (private repo
Exploded-views/crewkit); that file's sha256 and commit are in tools/evals.sha256. Changes: the
plugins and the memory-sync fakes are gone, the skill arm wraps skills/yo-human in a one-skill
plugin, replies are redacted before they are written, and a grade mode was added.

Usage:
  python3 -I tools/run_evals.py base|skill <repo> <out dir> <empty run dir> <id,id,...|all>
  python3 -I tools/run_evals.py grade <repo> <out dir> <id,id,...|all> [--regrade]

Without --regrade, grade keeps a grade already written (it resumes); with it, every grade is redone.

A skill-arm reply in which the skill was never loaded is run again, up to MAX_SAMPLES times; if it
never loads, the reply is written with a first line "INVALID: ..." and grade records it as invalid
(pass: null), never as FAIL. Tests: python3 -I tools/run_evals_test.py

Both arms: claude -p --model sonnet with no user/project settings, CLAUDE.md, memory or MCP, and
only the Skill tool (no Bash, so nothing runs and nothing is sent), stream-json output, a timeout
and an empty run directory. The skill arm also loads skills/yo-human through a temporary plugin
directory. Each case is a dry run: the agent says what it would run. The grader is a second
claude -p call with no tools that sees only the case and the transcript. Output is redacted
(ULIDs, timestamps, session ids, key-like strings); run evals/leakcheck.sh on it before committing.
"""
import json, pathlib, re, shutil, subprocess, sys, tempfile

COMMON = ["claude", "-p", "--model", "sonnet", "--no-session-persistence", "--setting-sources", "",
          "--strict-mcp-config"]
REDACT = [
    (re.compile(r"\b[0-9A-HJKMNP-TV-Z]{26}\b"), "<ULID>"),
    (re.compile(r"\b\d{4}-\d\d-\d\d[T ]\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:?\d\d)?\b"), "<TIMESTAMP>"),
    (re.compile(r"\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b"), "<SESSION-ID>"),
    (re.compile(r"\b(?:sk|pk|ghp|gho|xox[bap]|AKIA)[-_A-Za-z0-9]{12,}\b"), "<KEY>"),
    (re.compile(r"(?<![A-Za-z0-9+/_-])(?=[A-Za-z+/_-]*\d)[A-Za-z0-9+/_-]{32,}={0,2}"), "<LONG-TOKEN>"),
]


# MAX_SAMPLES is how many times a skill-arm case is run before a reply that never loaded the skill
# is written as INVALID: such a reply says nothing about the skill, so it is never graded FAIL.
MAX_SAMPLES = 3


def sample_once(cmd, prompt, run_dir):
    """Runs one arm call; returns the process, the final reply and the skills it loaded."""
    r = subprocess.run(cmd + ["--", prompt], cwd=run_dir, capture_output=True, text=True, timeout=400)
    result, skills = "", []
    for line in r.stdout.splitlines():
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        if ev.get("type") == "assistant":
            for b in ev["message"].get("content", []):
                if b.get("type") == "tool_use" and b.get("name") == "Skill":
                    skills.append(b.get("input", {}).get("skill") or b.get("input", {}).get("command"))
        if ev.get("type") == "result":
            result = ev.get("result", "")
    return r, result, skills


def redact(s):
    for pat, rep in REDACT:
        s = pat.sub(rep, s)
    return s


def load(repo, ids):
    cases = {c["id"]: c for c in json.loads((repo / "evals/cases.json").read_text())["cases"]}
    return cases, (list(cases) if ids == "all" else ids.split(","))


def run_arm(mode, repo, out_dir, run_dir, ids):
    cases, ids = load(repo, ids)
    out_dir.mkdir(parents=True, exist_ok=True)
    plugin = None
    extra = []
    if mode == "skill":
        plugin = pathlib.Path(tempfile.mkdtemp(prefix="yo-human-eval-"))
        (plugin / ".claude-plugin").mkdir()
        (plugin / ".claude-plugin/plugin.json").write_text(json.dumps({"name": "yo-human-eval", "version": "0.0.0"}))
        shutil.copytree(repo / "skills/yo-human", plugin / "skills/yo-human")
        extra = ["--plugin-dir", str(plugin)]
    try:
        for cid in ids:
            c = cases[cid]
            prompt = (f"Context: {c['context']}\n\n{c['prompt']}\n\n"
                      "This is a dry run: don't run commands. Say exactly what you would do next and "
                      "which commands you would run, in order.")
            cmd = COMMON + ["--tools", "Skill", "--output-format", "stream-json", "--verbose"] + extra
            # A skill-arm reply that never loaded the skill tests nothing about it: sample again.
            for sample in range(1, MAX_SAMPLES + 1 if mode == "skill" else 2):
                r, result, skills = sample_once(cmd, prompt, run_dir)
                if mode != "skill" or any("yo-human" in (s or "") for s in skills):
                    break
            text = f"skills used: {skills}\n\n{result}\n" + (f"\n[exit {r.returncode}]\n{r.stderr[-2000:]}" if r.returncode else "")
            if mode == "skill" and not any("yo-human" in (s or "") for s in skills):
                text = f"INVALID: the skill was not loaded in {MAX_SAMPLES} samples\n" + text
            (out_dir / f"{cid}.txt").write_text(redact(text))
            print(cid, r.returncode, skills, len(result), f"sample {sample}", flush=True)
    finally:
        if plugin:
            shutil.rmtree(plugin, ignore_errors=True)


def grade(repo, out_dir, ids, regrade=False):
    cases, ids = load(repo, ids)
    for cid in ids:
        c = cases[cid]
        raw = (out_dir / f"{cid}.txt").read_text()
        if raw.startswith("INVALID"):  # the skill was never loaded: no grade, and never a FAIL
            (out_dir / f"{cid}.grade.json").write_text(json.dumps({"invalid": True, "pass": None, "note": raw.splitlines()[0]}, indent=1) + "\n")
            print(cid, "INVALID", raw.splitlines()[0], flush=True)
            continue
        # blind: the arm header ("skills used: ...") is not shown to the grader
        transcript = re.sub(r"\Askills used:[^\n]*\n\n", "", raw)
        prompt = ("You grade one reply from an AI agent against a checklist. You see only the case and the reply. "
                  "Judge from the reply text alone.\n\n"
                  f"CASE CONTEXT: {c['context']}\n\nCASE PROMPT: {c['prompt']}\n\n"
                  f"MUST (each should be true of the reply):\n{json.dumps(c['must'], indent=1)}\n\n"
                  f"MUST NOT (each should be false of the reply):\n{json.dumps(c['must_not'], indent=1)}\n\n"
                  f"REPLY:\n<<<\n{transcript}\n>>>\n\n"
                  'Answer with one JSON object and nothing else: {"must":[true|false per MUST item, in order],'
                  '"must_not_violated":[true|false per MUST NOT item, in order],"note":"one short sentence"}')
        if not regrade and (out_dir / f"{cid}.grade.json").exists():  # resume: keep a grade already made
            continue
        r = subprocess.run(COMMON + ["--tools", "", "--output-format", "json", "--", prompt],
                           capture_output=True, text=True, timeout=400)
        txt = ""
        for line in reversed(r.stdout.splitlines()):  # the result object is the last JSON line
            try:
                txt = json.loads(line).get("result", "")
                break
            except (ValueError, AttributeError):
                continue
        v = {"error": txt[:200]}
        for m in re.finditer(r"\{", txt):  # the first JSON object in the reply, ignoring any text after it
            try:
                v = json.JSONDecoder().raw_decode(txt[m.start():])[0]
                break
            except ValueError:
                continue
        ok = (not v.get("error") and len(v.get("must", [])) == len(c["must"]) and all(v["must"])
              and len(v.get("must_not_violated", [])) == len(c["must_not"]) and not any(v["must_not_violated"]))
        v["pass"] = bool(ok)
        (out_dir / f"{cid}.grade.json").write_text(json.dumps(v, indent=1) + "\n")
        print(cid, "PASS" if ok else "FAIL", v.get("note", ""), flush=True)


if __name__ == "__main__":
    a = sys.argv[1:]
    if a[:1] in (["base"], ["skill"]) and len(a) == 5:
        run_arm(a[0], *map(pathlib.Path, a[1:4]), a[4])
    elif a[:1] == ["grade"] and len(a) in (4, 5) and (len(a) == 4 or a[4] == "--regrade"):
        grade(*map(pathlib.Path, a[1:3]), a[3], regrade=len(a) == 5)
    else:
        sys.exit(__doc__)
