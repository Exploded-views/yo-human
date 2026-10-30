# Skill evals

`evals/cases.json` holds the cases for `skills/yo-human`. Each has a stable `id`, a generic
`context` (canned `yo-human levels --json` output and exit codes, fake names only), a `prompt`,
and `must` / `must_not` lists a grader can check from the transcript alone.

Run (from the repo root; `<run dir>` must be an empty directory):

    python3 -I tools/run_evals.py base  . <out dir> <run dir> all   # no skill
    python3 -I tools/run_evals.py skill . <out dir> <run dir> all   # loads skills/yo-human
    python3 -I tools/run_evals.py grade . <out dir> all
    evals/leakcheck.sh <out dir>

Both arms use `claude -p --model sonnet --no-session-persistence --setting-sources ""
--strict-mcp-config --tools Skill`. The skill arm adds a temporary one-skill plugin. There is no
Bash tool, so nothing runs and nothing is sent. Each case is a dry run. The runner redacts
ULIDs, timestamps, session ids and key-like strings before writing; `leakcheck.sh` confirms it.
Results are recorded in `docs/tdd/skill-red.md`. A skill-arm case whose reply never loaded the
skill is run again, up to three times; if it still has not, the reply is marked `INVALID` and its
grade is `"invalid": true` with no pass or fail, since it says nothing about the skill. The
runner's own tests use a fake `claude`: `python3 -I tools/run_evals_test.py`.

`tools/run_evals.py` is adapted from crewkit's `evals/run_cases.py`; `tools/evals.sha256`
records the original's sha256 and commit as provenance (the file is not vendored unchanged,
since it is specific to crewkit's plugins).
