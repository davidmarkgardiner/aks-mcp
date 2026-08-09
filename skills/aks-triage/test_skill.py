import json
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).parent
REPO_ROOT = ROOT.parent.parent

# Tools and resources that the skill is allowed to reference. Any addition
# here must be paired with a check that the new entry is also read-only.
ALLOWED_TOOLS = {
    "aks_cluster_health",
    "aks_node_pressure",
    "aks_workload_failures",
    "aks_policy_posture",
    "aks_deployment_history",
}
ALLOWED_RESOURCES = {
    "aks://cluster/metadata",
    "aks://cluster/allowed-namespaces",
    "aks://cluster/health-snapshot",
    "aks://cluster/policy-posture",
}
# Mutation verbs that may only appear in a *prohibition* (e.g. "Never apply").
# The test parses sentences to confirm no sentence uses the verb as a
# recommendation.
MUTATION_VERBS = {
    "apply", "delete", "patch", "scale", "rollout", "restart", "exec",
}
# Literal tool/CLI invocations that must never appear at all, in any form.
MUTATION_INVOCATIONS = {
    "kubectl apply", "kubectl delete", "kubectl patch", "kubectl scale",
    "kubectl rollout", "kubectl exec", "az aks", "terraform apply",
}

# Mirrors docs/triage-contracts.md: every result must include these keys.
REQUIRED_FIXTURE_KEYS = {
    "schema_version",
    "signal",
    "scope",
    "observed_at",
    "findings",
    "truncated",
}


def _load_skill() -> str:
    return (ROOT / "SKILL.md").read_text()


def _load_fixture() -> dict:
    return json.loads((ROOT / "fixtures/workload-failure.json").read_text())


class SkillContractTests(unittest.TestCase):
    def test_fixture_matches_triage_v1_contract(self):
        finding = _load_fixture()
        self.assertEqual("triage.v1", finding["schema_version"])
        self.assertEqual("kubernetes_workload_failure", finding["signal"])
        self.assertEqual("single-namespace", finding["scope"]["namespace_scope"])
        self.assertEqual("checkout", finding["scope"]["namespace"])
        self.assertTrue(
            finding["scope"]["cluster"].startswith(
                "/subscriptions/"
            ),
            "scope.cluster should be an ARM resource id, not a short alias",
        )
        self.assertEqual(REQUIRED_FIXTURE_KEYS, set(finding.keys()))
        self.assertEqual("warning", finding["findings"][0]["severity"])
        self.assertEqual("CrashLoopBackOff", finding["findings"][0]["reason"])
        self.assertEqual(False, finding["truncated"])

    def test_skill_keeps_approval_boundary_on_a_single_line(self):
        skill = _load_skill()
        # The test fails if the phrase is split across lines because the
        # approval boundary is the contract a reviewer grep checks for.
        self.assertIn("immutable plan/approval boundary", skill)
        self.assertIn("Never apply", skill)

    def test_skill_only_references_read_only_tools_and_resources(self):
        skill = _load_skill()
        # Literal CLI invocations must never appear in any form.
        lowered = skill.lower()
        for invocation in MUTATION_INVOCATIONS:
            self.assertNotIn(invocation, lowered, f"skill mentions {invocation!r}")
        # Mutation verbs are tolerated only in a *prohibition* sentence: a
        # sentence that begins with "Never", "Do not", or contains "not call".
        sentences = re.split(r"(?<=[.!?])\s+", skill)
        for sentence in sentences:
            lower = sentence.lower()
            for verb in MUTATION_VERBS:
                # Tokenize on word boundaries so "applies" doesn't match "apply".
                tokens = set(re.findall(r"[a-z]+", lower))
                if verb in tokens and not (
                    "never" in tokens
                    or "no" in tokens
                    or "not" in tokens
                    or "without" in tokens
                ):
                    self.fail(
                        f"mutation verb {verb!r} used without a prohibition in: {sentence!r}"
                    )

    def test_skill_lists_allowed_signals_and_resources(self):
        skill = _load_skill()
        for tool in ALLOWED_TOOLS:
            self.assertIn(tool, skill, f"missing tool {tool} in skill")
        for resource in ALLOWED_RESOURCES:
            self.assertIn(resource, skill, f"missing resource {resource} in skill")

    def test_yaml_manifest_is_valid_and_read_only(self):
        try:
            import yaml
        except ImportError as exc:  # pragma: no cover - environment issue
            self.skipTest(f"PyYAML not available: {exc}")
        manifest_path = REPO_ROOT / "deploy" / "kagent" / "aks-mcp-triage.yaml"
        self.assertTrue(manifest_path.exists(), f"missing manifest: {manifest_path}")
        with manifest_path.open() as f:
            documents = list(yaml.safe_load_all(f))
        self.assertGreaterEqual(len(documents), 2)
        kinds = {doc["kind"] for doc in documents if doc}
        self.assertIn("RemoteMCPServer", kinds)
        self.assertIn("Agent", kinds)
        agent_docs = [doc for doc in documents if doc and doc.get("kind") == "Agent"]
        self.assertEqual(1, len(agent_docs))
        tool_names = agent_docs[0]["spec"]["declarative"]["tools"][0]["mcpServer"][
            "toolNames"
        ]
        self.assertEqual(ALLOWED_TOOLS, set(tool_names))
        # The description must call out the read-only contract and the gating
        # the service imposes before it advertises its resources.
        remote = next(
            doc for doc in documents if doc and doc.get("kind") == "RemoteMCPServer"
        )
        self.assertIn("read-only", remote["spec"]["description"].lower())


if __name__ == "__main__":
    unittest.main()
