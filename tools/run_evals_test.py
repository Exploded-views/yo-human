#!/usr/bin/env python3
"""Tests for run_evals.py, against a fake claude on PATH. Run: python3 -I tools/run_evals_test.py

A skill-arm reply in which the skill was never loaded tests nothing about the skill: it is INVALID,
re-sampled up to three times, and never graded FAIL.
"""
import json, os, pathlib, subprocess, sys, tempfile, unittest

REPO = pathlib.Path(__file__).resolve().parent.parent
CASE = json.loads((REPO / "evals/cases.json").read_text())["cases"][0]["id"]

# The fake answers an arm run with a stream-json reply that loads the skill only from the sample
# numbered LOAD_FROM (0: never), and counts every call in a file.
FAKE = r"""#!/bin/sh
dir=$(dirname "$0")
n=$(cat "$dir/calls" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$dir/calls"
case " $* " in
*" --output-format json "*) echo '{"result":"{\"must\":[],\"must_not_violated\":[],\"note\":\"graded\"}"}'; exit 0 ;;
esac
if [ "$LOAD_FROM" -gt 0 ] && [ "$n" -ge "$LOAD_FROM" ]; then
  echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"yo-human-eval:yo-human"}}]}}'
fi
echo '{"type":"result","result":"I would run yo-human flush."}'
"""


class InvalidSkillArm(unittest.TestCase):
    def run_skill_arm(self, load_from):
        tmp = pathlib.Path(tempfile.mkdtemp())
        bin_dir, out, run = tmp / "bin", tmp / "out", tmp / "run"
        for d in (bin_dir, run):
            d.mkdir()
        (bin_dir / "claude").write_text(FAKE)
        (bin_dir / "claude").chmod(0o755)
        env = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}", LOAD_FROM=str(load_from))
        r = subprocess.run([sys.executable, "-I", str(REPO / "tools/run_evals.py"), "skill", str(REPO), str(out), str(run), CASE],
                           env=env, capture_output=True, text=True, timeout=60)
        self.assertEqual(r.returncode, 0, r.stderr)
        calls = int((bin_dir / "calls").read_text())
        return env, out, calls

    def test_a_reply_that_loads_the_skill_late_is_resampled(self):
        _, out, calls = self.run_skill_arm(load_from=2)
        self.assertEqual(calls, 2)
        self.assertFalse((out / f"{CASE}.txt").read_text().startswith("INVALID"))

    def test_a_reply_that_never_loads_the_skill_is_invalid_not_failed(self):
        env, out, calls = self.run_skill_arm(load_from=0)
        self.assertEqual(calls, 3)
        self.assertTrue((out / f"{CASE}.txt").read_text().startswith("INVALID"))
        r = subprocess.run([sys.executable, "-I", str(REPO / "tools/run_evals.py"), "grade", str(REPO), str(out), CASE],
                           env=env, capture_output=True, text=True, timeout=60)
        grade = json.loads((out / f"{CASE}.grade.json").read_text())
        self.assertEqual((grade.get("invalid"), grade.get("pass")), (True, None), grade)
        self.assertIn("INVALID", r.stdout)
        self.assertNotIn("FAIL", r.stdout)


if __name__ == "__main__":
    unittest.main()
