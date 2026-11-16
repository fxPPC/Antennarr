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
    def test_clean_release_rejects_every_finding(self):
        matches = [{'vulnerability': {'severity': severity, 'fix': {'state': state}}}
                   for severity in ['Critical', 'High', 'Medium', 'Low', 'Unknown']
                   for state in ['fixed', 'not-fixed', 'unknown']]
        self.assertEqual(gate.blocking_findings({'matches': matches}, require_clean=True), matches)
        self.assertEqual(gate.blocking_findings({'matches': []}, require_clean=True), [])
    def test_review_is_exact_and_does_not_hide_new_findings(self):
        approved = {'id': 'CVE-test', 'package': 'ffmpeg', 'version': '1', 'severity': 'High'}
        match = {'vulnerability': {'id': 'CVE-test', 'severity': 'High', 'fix': {'state': ''}},
                 'artifact': {'name': 'ffmpeg', 'version': '1'}}
        self.assertEqual(gate.blocking_findings({'matches': [match]}, True, [approved]), [])
        self.assertEqual(gate.blocking_findings({'matches': [match]}, True), [match])
        match['artifact']['version'] = '2'
        self.assertEqual(gate.blocking_findings({'matches': [match]}, True, [approved]), [match])
        match['artifact']['version'] = '1'
        match['vulnerability']['fix']['state'] = 'fixed'
        self.assertEqual(gate.blocking_findings({'matches': [match]}, True, [approved]), [match])

    def test_incomplete_report_fails_closed(self):
        for report in [{}, {'matches': None}, {'matches': {}}]:
            with self.assertRaises(ValueError):
                gate.blocking_findings(report)
    def test_valid_empty_report(self):
        self.assertEqual(gate.blocking_findings({'matches': []}), [])

unittest.main()
