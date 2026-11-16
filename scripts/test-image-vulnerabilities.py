import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('gate', Path(__file__).with_name('check-image-vulnerabilities.py'))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)

class GateTests(unittest.TestCase):
    def test_only_fixable_high_and_critical_block(self):
        matches = [{'vulnerability': {'severity': severity, 'fix': {'state': state}}}
                   for severity in ['Critical', 'High', 'Medium', 'Low']
                   for state in ['fixed', 'not-fixed', 'unknown']]
        self.assertEqual(len(gate.blocking_findings({'matches': matches})), 2)
    def test_incomplete_report_fails_closed(self):
        for report in [{}, {'matches': None}, {'matches': {}}]:
            with self.assertRaises(ValueError):
                gate.blocking_findings(report)
    def test_valid_empty_report(self):
        self.assertEqual(gate.blocking_findings({'matches': []}), [])

unittest.main()
