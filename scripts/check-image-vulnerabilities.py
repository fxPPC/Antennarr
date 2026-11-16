#!/usr/bin/env python3
"""Retain every finding in the report; block fixable high/critical packages."""
import json
import sys


def blocking_findings(report):
    if not isinstance(report.get('matches'), list):
        raise ValueError('Missing or invalid vulnerability matches; refusing an incomplete scan')
    return [m for m in report['matches']
            if m['vulnerability']['severity'].lower() in ('high', 'critical')
            and m['vulnerability'].get('fix', {}).get('state') == 'fixed']


if __name__ == '__main__':
    with open(sys.argv[1], encoding='utf-8') as source:
        report = json.load(source)
    blocked = blocking_findings(report)
    for match in blocked:
        v, package = match['vulnerability'], match['artifact']
        print(f"{v['id']}: {package['name']} {package['version']} -> {', '.join(v['fix'].get('versions', []))}")
    print(f"{len(report.get('matches', []))} findings; {len(blocked)} fixable high/critical findings")
    sys.exit(bool(blocked))
