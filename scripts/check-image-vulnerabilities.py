#!/usr/bin/env python3
"""Retain every finding; publication can require an entirely clean report."""
import argparse
import json
import sys


def blocking_findings(report, require_clean=False):
    if not isinstance(report.get('matches'), list):
        raise ValueError('Missing or invalid vulnerability matches; refusing an incomplete scan')
    if require_clean:
        return report['matches']
    return [m for m in report['matches']
            if m['vulnerability']['severity'].lower() in ('high', 'critical')
            and m['vulnerability'].get('fix', {}).get('state') == 'fixed']


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('report')
    parser.add_argument('--require-clean', action='store_true', help='Reject every detected vulnerability, including unfixed and low-severity findings')
    args = parser.parse_args()
    with open(args.report, encoding='utf-8') as source:
        report = json.load(source)
    blocked = blocking_findings(report, args.require_clean)
    for match in blocked:
        v, package = match['vulnerability'], match['artifact']
        print(f"{v['id']}: {package['name']} {package['version']} -> {', '.join(v['fix'].get('versions', []))}")
    print(f"{len(report.get('matches', []))} findings; {len(blocked)} blocking findings")
    sys.exit(bool(blocked))
