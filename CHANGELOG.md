# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- M0 scaffold: Go module and `yo-human version` CLI skeleton, `make` targets
  (`build smoke test lint lint-rules-test tools hooks workflows cover floors perf secrets
  secrets-test vuln ci ci-base`), GitHub Actions CI on hosted runners, workflow lint,
  repo size and complexity lint rules, per-package coverage floors,
  performance budgets, gitleaks secret scan, and `docs/tdd/` RED records.
- Repo docs and templates: MIT license, security policy, contributing guide,
  CODEOWNERS, pull request template and task and decision issue forms.

### Changed

- README rewritten for new readers: why yo-human exists, the planned M1 CLI
  (marked as not shipped), a message-path diagram, build-from-source steps and
  the milestone roadmap. Theme-aware banner in `docs/assets/`.
