#!/usr/bin/env python3
"""Aggregate privacy-safe BAP shadow JSONL into human-review suggestions."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable


def _objects(paths: Iterable[Path]) -> tuple[list[dict[str, Any]], list[dict[str, str]]]:
    records: list[dict[str, Any]] = []
    errors: list[dict[str, str]] = []
    for path in paths:
        try:
            with path.open("r", encoding="utf-8-sig") as stream:
                for number, line in enumerate(stream, 1):
                    if not line.strip():
                        continue
                    try:
                        value = json.loads(line)
                        if isinstance(value, dict):
                            value["__source_file"] = str(path)
                            records.append(value)
                    except (json.JSONDecodeError, UnicodeDecodeError) as error:
                        errors.append({"file": str(path), "line": str(number), "error": str(error)})
        except OSError as error:
            errors.append({"file": str(path), "line": "0", "error": str(error)})
    return records, errors


def analyze(directory: Path, min_count: int = 2, enable_ml: bool = True) -> dict[str, Any]:
    paths = sorted(path for path in directory.rglob("*.jsonl") if path.is_file())
    records, parse_errors = _objects(paths)
    outcomes: dict[tuple[str, str, str], str] = {}
    for record in records:
        if record.get("event_type") == "tool_outcome":
            key = (str(record.get("session_id", "")), str(record.get("workload_id", "")), str(record.get("tool_use_id", "")))
            outcomes[key] = str(record.get("outcome", "unknown"))
    central_operations = {
        (str(record.get("session_id", "")), str(record.get("workload_id", "")), str(record.get("tool_use_id", "")))
        for record in records
        if record.get("event_type") == "authorization_decision"
    }
    central_operations.discard(("", "", ""))

    groups: dict[tuple[str, str, str, str, str], dict[str, Any]] = defaultdict(
        lambda: {"observations": 0, "successes": 0, "failures": 0, "sessions": set(), "sources": set()}
    )
    learned_rows: list[tuple[str, str, str, str, str]] = []
    seen: set[str] = set()
    prompt_signals: dict[str, int] = defaultdict(int)
    for record in records:
        if record.get("event") == "prompt_intent_classification" and record.get("enforcement_mode") == "shadow" and record.get("decision") == "matched":
            for rule_id in str(record.get("reason_code", "")).split(","):
                if rule_id:
                    prompt_signals[rule_id] += 1
            continue

        central = record.get("event_type") == "authorization_decision"
        edge = record.get("event") == "authorization_result"
        if not (central or edge) or record.get("enforcement_mode") != "shadow":
            continue
        operation = (str(record.get("session_id", "")), str(record.get("workload_id", "")), str(record.get("tool_use_id", "")))
        if edge and operation in central_operations:
            # The Service copy is signed and authoritative. Do not count the
            # corresponding Edge observation again when both are collected.
            continue
        evaluated_allowed = record.get("evaluated_allowed") if central else record.get("evaluated_decision") == "allow"
        effective_allowed = record.get("allowed") if central else record.get("decision") == "allow"
        if evaluated_allowed is not False or effective_allowed is not True:
            continue
        dedupe = str(record.get("event_id") or record.get("trace_id") or "")
        if dedupe and dedupe in seen:
            continue
        if dedupe:
            seen.add(dedupe)
        principal = str(record.get("principal") or record.get("asserted_user") or "unverified")
        action = str(record.get("action") or "unknown")
        tool = str(record.get("tool") or "unknown")
        reason = str(record.get("evaluated_reason_code") or "unknown")
        target_key = str(record.get("target_summary") or "unspecified")
        key = (principal, action, tool, reason, target_key)
        learned_rows.append(key)
        group = groups[key]
        group["observations"] += 1
        group["sessions"].add(str(record.get("session_id") or "unknown"))
        group["sources"].add(Path(str(record["__source_file"])).name)
        if outcomes.get(operation) == "success":
            group["successes"] += 1
        elif outcomes.get(operation) == "failure":
            group["failures"] += 1

    feature_counts = [Counter(row[index] for row in learned_rows) for index in range(5)]
    feature_cardinalities = [max(1, len(counts)) for counts in feature_counts]
    learned_total = len(learned_rows)
    recommendations = []
    for key, group in groups.items():
        if group["observations"] < min_count:
            continue
        principal, action, tool, reason, target_key = key
        target_class = target_key.split(":", 1)[0] if ":" in target_key else target_key
        identity = "|".join(key).encode("utf-8")
        candidate = {
            "candidate_id": "shadow-" + hashlib.sha256(identity).hexdigest()[:16],
            "status": "human_review_required",
            "principal": principal,
            "action": action,
            "tool": tool,
            "target_class": target_class,
            "target_key": target_key,
            "evaluated_reason_code": reason,
            "observations": group["observations"],
            "distinct_sessions": len(group["sessions"]),
            "successful_outcomes": group["successes"],
            "failed_outcomes": group["failures"],
            "source_files": sorted(group["sources"]),
            "recommendation": "Consider a narrowly scoped permit candidate only after authoritative identity, resource-owner, failure, and counterexample review.",
            "proposed_review_scope": {
                "effect": "permit_candidate",
                "observed_principal_to_verify": principal,
                "action": action,
                "tool": tool,
                "resource_class": target_class,
                "resource_key": target_key,
                "required_controls": [
                    "resolve subject membership from authoritative IAM",
                    "obtain resource-owner approval",
                    "review failures and counterexamples",
                    "add positive and bypass tests",
                    "activate only through signed policy lifecycle",
                ],
                "automatic_activation": False,
            },
        }
        if enable_ml and learned_total:
            # An explainable categorical density estimator: each field's
            # distribution is learned from this input corpus with Laplace
            # smoothing. Higher surprisal means the combination is less usual.
            probabilities = [
                (feature_counts[index][value] + 1) / (learned_total + feature_cardinalities[index])
                for index, value in enumerate(key)
            ]
            novelty_bits = sum(-math.log2(probability) for probability in probabilities) / len(probabilities)
            evidence = math.log2(1 + group["observations"]) + math.log2(1 + len(group["sessions"]))
            outcome_total = group["successes"] + group["failures"]
            success_ratio = group["successes"] / outcome_total if outcome_total else 0.0
            candidate["learned_ranking"] = {
                "model": "categorical_density_v1",
                "novelty_bits": round(novelty_bits, 4),
                "evidence_score": round(evidence, 4),
                "successful_outcome_ratio": round(success_ratio, 4),
                "review_priority_score": round(novelty_bits + evidence + success_ratio, 4),
                "explanation": "Priority combines learned categorical rarity, repeated evidence, distinct sessions, and observed successful outcomes; it never decides access.",
            }
        recommendations.append(candidate)
    recommendations.sort(key=lambda item: (-item.get("learned_ranking", {}).get("review_priority_score", 0), -item["observations"], item["candidate_id"]))

    input_manifest = []
    for path in paths:
        try:
            input_manifest.append({"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
        except OSError:
            pass
    return {
        "schema_version": 1,
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "mode": "offline_shadow_recommendation",
        "activation": "never_automatic",
        "learning_model": {
            "enabled": enable_ml,
            "name": "categorical_density_v1" if enable_ml else "disabled",
            "training_records": learned_total if enable_ml else 0,
            "purpose": "Explainable offline ranking for human review only; never an authorization decision.",
        },
        "privacy": "Consumes structured BAP metadata only; raw prompts and command text are not required or emitted.",
        "files_scanned": len(paths),
        "records_scanned": len(records),
        "parse_errors": parse_errors,
        "prompt_rule_signals": [{"rule_id": key, "observations": prompt_signals[key]} for key in sorted(prompt_signals)],
        "recommendations": recommendations,
        "input_manifest": input_manifest,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="Directory recursively containing BAP JSONL files")
    parser.add_argument("--output", type=Path, help="Write JSON report to this path; stdout is used when omitted")
    parser.add_argument("--min-count", type=int, default=2, help="Minimum repeated observations per suggestion")
    parser.add_argument("--disable-ml", action="store_true", help="Disable learned categorical ranking and emit deterministic counts only")
    args = parser.parse_args()
    if not args.directory.is_dir():
        parser.error(f"directory does not exist: {args.directory}")
    if args.min_count < 1:
        parser.error("--min-count must be at least 1")
    report = analyze(args.directory.resolve(), args.min_count, enable_ml=not args.disable_ml)
    payload = json.dumps(report, indent=2, sort_keys=True) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(payload, encoding="utf-8")
    else:
        print(payload, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
