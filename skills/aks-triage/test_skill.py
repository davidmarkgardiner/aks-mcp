import json
import re
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).parent
SKILL = ROOT / "SKILL.md"
FIXTURES = ROOT / "fixtures"
MANIFEST = Path(__file__).parents[2] / "deploy" / "kagent" / "aks-mcp-triage.yaml"

FORBIDDEN = [
    "-----BEGIN",
    "-----END",
    "kubeconfig",
    "client-certificate-data",
    "client-key-data",
    "token:",
    "password",
    "AZURE_CLIENT_SECRET",
    "AZURE_FEDERATED_TOKEN_FILE",
]

READ_ONLY_TOOLS = {
    "aks_cluster_health",
    "aks_node_pressure",
    "aks_workload_failures",
    "aks_policy_posture",
    "aks_deployment_history",
}
MUTATION_VERBS = {"apply", "create", "delete", "patch", "restart", "scale", "cordon", "drain"}


class SkillContractTests(unittest.TestCase):
    def test_skill_markdown_has_required_sections(self):
        text = SKILL.read_text()
        for section in [
            "## Purpose and scope",
            "## Stages",
            "## Tool and resource mapping",
            "## Severity classification",
            "## Approval boundary and escalation",
            "## Output format",
        ]:
            self.assertIn(section, text, f"Missing section {section}")
        self.assertIn("immutable plan/approval boundary", text)
        self.assertIn("Mutation is unavailable to the triage role", text)

    def test_skill_forbids_mutation_and_credentials(self):
        text = SKILL.read_text().lower()
        for verb in MUTATION_VERBS:
            # The skill must explicitly forbid each mutation verb.
            self.assertIn(verb, text, f"Skill does not mention forbidden verb {verb}")
        for forbidden in FORBIDDEN:
            self.assertNotIn(forbidden.lower(), text,
                               f"Skill must not contain credential material: {forbidden}")

    def test_workload_failure_fixture_is_valid_triage_v1(self):
        finding = json.loads((FIXTURES / "workload-failure.json").read_text())
        self.assertEqual("triage.v1", finding["schema_version"])
        self.assertEqual("kubernetes_workload_failure", finding["signal"])
        self.assertIn("scope", finding)
        self.assertIn("observed_at", finding)
        self.assertIsInstance(finding["findings"], list)
        self.assertEqual("warning", finding["findings"][0]["severity"])
        self.assertIn("truncated", finding)
        self.assertFalse(finding["truncated"])

    def test_empty_fixture_is_observation_not_health(self):
        empty = json.loads((FIXTURES / "empty-result.json").read_text())
        self.assertEqual("triage.v1", empty["schema_version"])
        self.assertEqual("kubernetes_workload_failure", empty["signal"])
        self.assertEqual([], empty["findings"])
        self.assertFalse(empty["truncated"])

    def test_permission_failure_fixture_is_safe(self):
        error = json.loads((FIXTURES / "permission-error.json").read_text())
        self.assertEqual("triage.v1", error["schema_version"])
        self.assertEqual("permission_error", error["signal"])
        self.assertNotIn("secrets", error["error"].lower())
        self.assertNotIn("token", error["error"].lower())
        self.assertIn("access denied", error["error"].lower())

    def test_manifest_is_valid_multidoc_yaml(self):
        docs = list(yaml.safe_load_all(MANIFEST.read_text()))
        self.assertEqual(2, len(docs))
        kinds = {doc["kind"] for doc in docs if doc}
        self.assertIn("RemoteMCPServer", kinds)
        self.assertIn("Agent", kinds)

    def test_agent_manifest_is_read_only(self):
        docs = list(yaml.safe_load_all(MANIFEST.read_text()))
        agent = next(doc for doc in docs if doc and doc.get("kind") == "Agent")
        tool_names = set(agent["spec"]["declarative"]["tools"][0]["mcpServer"]["toolNames"])
        self.assertEqual(READ_ONLY_TOOLS, tool_names)
        system = agent["spec"]["declarative"]["systemMessage"].lower()
        for verb in MUTATION_VERBS:
            self.assertIn(verb, system, f"System message must forbid {verb}")

    def test_no_credential_material_in_skill_files(self):
        for path in [SKILL, *FIXTURES.glob("*.json")]:
            text = path.read_text().lower()
            for forbidden in FORBIDDEN:
                self.assertNotIn(forbidden.lower(), text,
                                 f"Credential material found in {path}: {forbidden}")

    def test_skill_return_sections_are_documented(self):
        text = SKILL.read_text()
        for section in ["TL;DR", "Evidence", "Impact", "Likely cause",
                        "Proposed runbook", "Approval boundary", "Verification", "Confidence"]:
            self.assertIn(f"`## {section}`", text,
                          f"Output section `## {section}` must be documented")


if __name__ == "__main__":
    unittest.main()
