# Changelog

All notable changes to cc-filter are documented in this file.

## [Unreleased]

### Added
- BAP Service-owned, versioned policy source and dedicated Ed25519-signed policy bundles
- Authenticated Edge synchronization with update, force-update, and kill-switch directives
- Local BAP Edge Cedar decisions with expiry, offline lease, rollback, equivocation, and default-deny checks
- Structured centrally managed command rules, including `git status --short` and `ls -al`
- Durable Edge-decision spooling, asynchronous central audit ingestion, and optional per-device mTLS
- Unit, live MVP, offline-operation, tamper, rule-removal, directive, mTLS, and database-failure coverage
- Data-driven command/bypass corpus and an HTTPS Service-to-Edge rollout lifecycle gate
- Privacy-safe exact Claude hook schema capture, replay, hash manifest, model-equivalence, and policy-digest certification
- Read-only `Show-BapStatus.ps1` view of control-plane and Edge policy/lease/audit posture

### Changed
- BAP Service is the rule control plane; BAP Edge is the traffic data plane and no longer needs a network authorization decision per tool call
- Endpoint configuration no longer accepts policy profiles or allow registries
- MVP architecture, installation, testing, certificate, deployment, and roadmap documentation now describe the signed-bundle model
- The modeled Claude contract corpus now evaluates 62 tool/schema cases through normalization and the active local Cedar policy

## [v0.0.6] - 2026-03-12

### Fixed
- Fix default rules not loading when running as a Claude Code hook (#6)
- Embed default rules into binary so all patterns are available regardless of working directory

### Added
- Bearer token filtering pattern
- CI pipeline with GitHub Actions for running tests on push and PRs

## [v0.0.5] - 2026-03-11

### Fixed
- Fix permissions prompt and deny list override (#5)

## [v0.0.4] - 2026-01-22

### Added
- Configurable file redaction via `redact_files` config
- UserPromptSubmit UX improvements: inline display and clipboard copy
- Unit tests and API limitation documentation
- Windows support documentation

### Fixed
- UserPromptSubmit hook not blocking content correctly

## [v0.0.3] - 2025-09-13

### Fixed
- Separate build and release jobs to avoid race condition in CI

## [v0.0.2] - 2025-09-13

### Changed
- Updated release workflow

## [v0.0.1] - 2025-09-13

### Added
- Initial release
- Stdin filtering with configurable regex patterns
- Claude Code hook support (PreToolUse, UserPromptSubmit, SessionEnd)
- Default rules for API keys, secret keys, access tokens, passwords, database URLs, JWT tokens, private keys, client secrets, auth tokens, OpenAI keys, Slack tokens, and environment variables
- File blocking for `.env`, `.pem`, `.key`, and other sensitive files
- Search and command blocking
- User config (`~/.cc-filter/config.yaml`) and project config (`./config.yaml`) support with merge strategy
