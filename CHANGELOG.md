# Changelog

All notable changes to cc-filter are documented in this file.

## [Unreleased]

### Added
- Signed, configurable session capability mappings, ordered composition
  forbids, rolling budgets, lifetime/idle limits, and bounded per-session state
- Cross-process atomic session ledgers for multiple concurrent Claude hook
  instances, including pending reservation and outcome completion semantics
- Centrally enforced Agent STS intent nonces and transactional per-intent
  issuance budgets in memory and MySQL schema version 4
- `Test-SessionCapabilities.ps1` native/Docker/Podman security gate with a
  hash-bound evidence manifest for company CI attestation
- Neutral eight-turn session-accretion fixture and deterministic direct-BAP,
  native-local-model, and zero-argument company Sonnet test runners; the
  observation explicitly reports uncovered policy mappings instead of treating
  model behavior as enforcement evidence
- Resumable Claude process support: `SessionEnd` no longer discards a session's
  workload/capability ledger, and Windows UTF-8 BOM hook input is accepted
- Native runtime support for audit verification, readable per-session decision
  timelines, and status inspection; native verification now records the exact
  denied-Bash then allowed-Read route change as two distinct tool calls
- Audit timelines tolerate valid event variants with optional action, tool,
  reason, target, session, or outcome fields under PowerShell strict mode
- One-click native Windows local test launcher for BAP Service, BAP Edge, signed
  policy verification, and temporary project-local Claude hooks
- Native Windows Go fallback and explicit Windows/Linux compilation targets for
  BAP Edge and BAP Service when Docker/Podman is unavailable
- Versioned company artifact build with internal digest-pinned base-image inputs,
  checksums, source manifest, and GitHub build provenance
- Bounded grant cache and durable audit spool with atomic delivery claims,
  stale-claim recovery, privacy-safe spool metrics, and status reporting
- BAP Service-owned, versioned policy source and dedicated Ed25519-signed policy bundles
- Authenticated Edge synchronization with update, force-update, and kill-switch directives
- Local BAP Edge Cedar decisions with expiry, offline lease, rollback, equivocation, and default-deny checks
- Structured centrally managed command rules, including `git status --short` and `ls -al`
- Durable Edge-decision spooling, asynchronous central audit ingestion, and optional per-device mTLS
- Unit, live MVP, offline-operation, tamper, rule-removal, directive, mTLS, and database-failure coverage
- Data-driven command/bypass corpus and an HTTPS Service-to-Edge rollout lifecycle gate
- Privacy-safe exact Claude hook schema capture, replay, hash manifest, model-equivalence, and policy-digest certification
- Read-only `Show-BapStatus.ps1` view of control-plane and Edge policy/lease/audit posture
- Signed `manual-only` command policy for database, SSH, and kubectl clients,
  with a non-echoing employee handoff and distinct auditable reason code
- Locally evaluated, centrally signed `UserPromptSubmit` intent rules for early
  manual-execution guidance on database, remote-shell, and cluster work
- Native one-click positive and negative prompt-classifier verification while
  preserving cc-filter secret blocking as the first prompt stage
- Native one-click Claude launch now preserves the local bridge/model and
  reduced-tool settings; `-UseCompanyClaude` selects normal company login
  without carrying the local bridge URL or demo credential
- Direct command-shaped prompts beginning with a governed database, SSH, or
  kubectl client now receive the same signed manual-only intent advisory
- Native local-hook launch now refuses to mix with `allowManagedHooksOnly`, and
  the managed local-model launcher detects a stale installed Edge binary
- Launcher diagnostics now stop cleanly without a PowerShell stack trace;
  Claude EXE/CMD discovery and company-mode removal of local bridge overrides
  no longer assume a particular installation shape or API-key variable
- Added administrator-only `Install-ManagedSettings.ps1 -Undo`, which verifies
  and removes only BAP's managed-settings drop-in while retaining reinstallable
  Edge files, trust material, and the machine credential
- Native one-click tests now use retained per-run Service/Edge state, keys,
  logs, and JSONL audit chains, preventing concurrent or stale development runs
  from creating competing audit-chain heads
- Removed duplicate Claude hook notices by emitting denial text only through
  `permissionDecisionReason` and prompt guidance only through
  `additionalContext`, while preserving any parent cc-filter message
- Added `Test-MVP0.ps1 -Runtime Native` for container-free company testing of
  portable Go, signed-policy, native Service/Edge, prompt, and fixture checks,
  with explicit reporting of MySQL/container-only checks that were not run
- Added container-free native company fixture capture with one safe Sonnet
  compatibility call; deny coverage remains in deterministic policy tests
- Added one-command interactive company fixture capture for managed launchers
  that intentionally do not accept Claude CLI arguments
- Changed `-UseCompanyClaude` launchers to interactive zero-argument mode by
  default; CLI-capable company installations require explicit opt-in
- Reduced the required live company-model gate to one safe Sonnet tool call;
  destructive and manual-only denials remain deterministic Edge/policy tests
- Added `Demo-BapNative.ps1` as a one-command company demonstration of the
  strict native gate, fixture evidence, and signed audit-chain integrity
- Unified demonstrations under `Demo-Bap.ps1 -Runtime
  Native|Docker|Podman|Auto`; every mode now requires company fixtures

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
