import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from analyze_shadow import analyze


class AnalyzeShadowTests(unittest.TestCase):
    def test_correlates_shadow_decisions_without_prompt_or_command_content(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            records = []
            for index in range(2):
                records.extend([
                    {"event_id": f"d{index}", "event_type": "authorization_decision", "enforcement_mode": "shadow", "evaluated_allowed": False, "allowed": True, "evaluated_reason_code": "MANUAL_EXECUTION_REQUIRED", "action": "command.execute", "tool": "Bash", "principal": "pilot-edge", "session_id": f"s{index}", "workload_id": "w", "tool_use_id": f"t{index}", "target_summary": "command-sha256:secret-hash"},
                    {"event_type": "tool_outcome", "session_id": f"s{index}", "workload_id": "w", "tool_use_id": f"t{index}", "outcome": "success"},
                ])
            records.append({"event": "prompt_intent_classification", "enforcement_mode": "shadow", "decision": "matched", "reason_code": "prompt.manual.database", "prompt": "must-not-appear"})
            records.append({"event": "authorization_result", "enforcement_mode": "shadow", "evaluated_decision": "deny", "decision": "allow", "trace_id": "edge-copy", "action": "command.execute", "tool": "Bash", "session_id": "s0", "workload_id": "w", "tool_use_id": "t0", "target_summary": "command-sha256:secret-hash"})
            (root / "audit.jsonl").write_text("".join(json.dumps(item) + "\n" for item in records), encoding="utf-8")
            report = analyze(root, min_count=2)
            self.assertEqual(len(report["recommendations"]), 1)
            candidate = report["recommendations"][0]
            self.assertEqual(candidate["observations"], 2)
            self.assertEqual(candidate["successful_outcomes"], 2)
            self.assertEqual(candidate["target_class"], "command-sha256")
            self.assertEqual(candidate["target_key"], "command-sha256:secret-hash")
            self.assertEqual(candidate["learned_ranking"]["model"], "categorical_density_v1")
            self.assertGreater(candidate["learned_ranking"]["review_priority_score"], 0)
            self.assertEqual(candidate["proposed_review_scope"]["effect"], "permit_candidate")
            self.assertFalse(candidate["proposed_review_scope"]["automatic_activation"])
            self.assertEqual(report["learning_model"]["training_records"], 2)
            self.assertEqual(report["prompt_rule_signals"][0]["rule_id"], "prompt.manual.database")
            self.assertNotIn("must-not-appear", json.dumps(report))

    def test_ignores_enforced_and_non_overridden_decisions(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "edge.jsonl").write_text(
                json.dumps({"event": "authorization_result", "enforcement_mode": "enforce", "evaluated_decision": "deny", "decision": "deny"}) + "\n" +
                json.dumps({"event": "authorization_result", "enforcement_mode": "shadow", "evaluated_decision": "deny", "decision": "deny"}) + "\n",
                encoding="utf-8",
            )
            self.assertEqual(analyze(root, min_count=1)["recommendations"], [])

    def test_learning_ranker_can_be_disabled(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "edge.jsonl").write_text(
                json.dumps({"event": "authorization_result", "enforcement_mode": "shadow", "evaluated_decision": "deny", "decision": "allow", "trace_id": "t1"}) + "\n",
                encoding="utf-8",
            )
            report = analyze(root, min_count=1, enable_ml=False)
            self.assertFalse(report["learning_model"]["enabled"])
            self.assertNotIn("learned_ranking", report["recommendations"][0])


    def test_suppresses_edge_observability_matching_central_trace(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            central_records = [
                {
                    "event_id": "dec-1",
                    "event_type": "authorization_decision",
                    "trace_id": "shared-trace-abc",
                    "enforcement_mode": "shadow",
                    "evaluated_allowed": False,
                    "allowed": True,
                    "evaluated_reason_code": "NO_MATCHING_POLICY",
                    "action": "command.execute",
                    "tool": "Bash",
                    "principal": "pilot-user",
                    "session_id": "s1",
                    "workload_id": "w1",
                    "tool_use_id": "t1",
                    "target_summary": "command-sha256:target-hash",
                },
                {
                    "event_type": "tool_outcome",
                    "session_id": "s1",
                    "workload_id": "w1",
                    "tool_use_id": "t1",
                    "outcome": "success",
                },
            ]
            # Realistic EdgeEvent: has trace_id, but lacks session_id, workload_id, tool_use_id, target_summary
            edge_records = [
                {
                    "event": "authorization_result",
                    "trace_id": "shared-trace-abc",
                    "enforcement_mode": "shadow",
                    "evaluated_decision": "deny",
                    "decision": "allow",
                    "action": "command.execute",
                    "tool": "Bash",
                }
            ]
            (root / "service-audit.jsonl").write_text("".join(json.dumps(r) + "\n" for r in central_records), encoding="utf-8")
            (root / "edge-observability.jsonl").write_text("".join(json.dumps(r) + "\n" for r in edge_records), encoding="utf-8")
            report = analyze(root, min_count=1)
            self.assertEqual(len(report["recommendations"]), 1)
            candidate = report["recommendations"][0]
            self.assertEqual(candidate["observations"], 1)
            self.assertEqual(candidate["target_key"], "command-sha256:target-hash")
            self.assertEqual(candidate["principal"], "pilot-user")

    def test_keeps_distinct_action_in_reused_trace(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            central = {
                "event_id": "dec-command",
                "event_type": "authorization_decision",
                "trace_id": "shared-trace",
                "enforcement_mode": "shadow",
                "evaluated_allowed": False,
                "allowed": True,
                "evaluated_reason_code": "NO_MATCHING_POLICY",
                "action": "command.execute",
                "tool": "Bash",
                "principal": "pilot-user",
                "target_summary": "command-sha256:target-hash",
            }
            edge = {
                "event": "authorization_result",
                "trace_id": "shared-trace",
                "enforcement_mode": "shadow",
                "evaluated_decision": "deny",
                "decision": "allow",
                "evaluated_reason_code": "NO_MATCHING_POLICY",
                "action": "file.write",
                "tool": "Write",
            }
            # Edge sorts before Service to prove input order does not make the
            # distinct Service authorization disappear.
            (root / "edge-observability.jsonl").write_text(json.dumps(edge) + "\n", encoding="utf-8")
            (root / "service-audit.jsonl").write_text(json.dumps(central) + "\n", encoding="utf-8")
            report = analyze(root, min_count=1)
            scopes = {(item["action"], item["tool"], item["target_key"]) for item in report["recommendations"]}
            self.assertEqual(
                scopes,
                {
                    ("command.execute", "Bash", "command-sha256:target-hash"),
                    ("file.write", "Write", "unspecified"),
                },
            )


if __name__ == "__main__":
    unittest.main()
