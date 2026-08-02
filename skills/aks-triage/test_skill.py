import json
from pathlib import Path
import unittest

ROOT = Path(__file__).parent

class SkillContractTests(unittest.TestCase):
    def test_fixture_maps_to_a_non_mutating_escalation(self):
        finding = json.loads((ROOT / "fixtures/workload-failure.json").read_text())
        self.assertEqual("triage.v1", finding["schema_version"])
        self.assertEqual("warning", finding["findings"][0]["severity"])
        skill = (ROOT / "SKILL.md").read_text()
        self.assertIn("immutable plan/approval boundary", skill)
        self.assertIn("Never apply", skill)

if __name__ == "__main__":
    unittest.main()
