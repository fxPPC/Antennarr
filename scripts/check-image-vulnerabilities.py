#!/usr/bin/env python3
"""Retain every finding; publication can require an entirely clean report."""
import argparse
import json
import sys


def approved_finding(match, approvals):
    """Approval covers only the reviewed package, severity, and release.

    A newly available fix invalidates approval. Reports retain every finding.
    """
    vulnerability, package = match['vulnerability'], match['artifact']
    fix = vulnerability.get('fix', {})
    if fix.get('state') == 'fixed' or fix.get('versions'):
        return False
    identity = {
        'id': vulnerability['id'], 'package': package['name'],
        'version': package['version'], 'severity': vulnerability['severity'],
    }
    return identity in approvals


def exact_review(matches, approvals):
    from collections import Counter
    expected = Counter((r['id'], r['package'], r['version'], r['severity']) for r in approvals)
    actual = Counter((m['vulnerability']['id'], m['artifact']['name'], m['artifact']['version'],
                      m['vulnerability']['severity']) for m in matches)
    return actual == expected


def blocking_findings(report, require_clean=False, approvals=()):
    if not isinstance(report.get('matches'), list):
        raise ValueError('Missing or invalid vulnerability matches; refusing an incomplete scan')
    if require_clean:
        return [m for m in report['matches'] if not approvals or not approved_finding(m, approvals)]
    return [m for m in report['matches']
            if m['vulnerability']['severity'].lower() in ('high', 'critical')
            and m['vulnerability'].get('fix', {}).get('state') == 'fixed']


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--require-clean', action='store_true', help='Reject every detected vulnerability, including unfixed and low-severity findings')
    parser.add_argument('--release-version', help='Use only the approvals recorded for this exact release')
    args = parser.parse_args()
    review = {}
    approvals = []
    if args.release_version:
        from pathlib import Path
        reviews = json.loads(Path(__file__).with_name('release-security-reviews.json').read_text())
        review = reviews.get(args.release_version, {})
        approvals = review.get('findings', [])
    report = json.load(sys.stdin)
    blocked = blocking_findings(report, args.require_clean, approvals)
    if args.require_clean and review.get('require_exact') and not exact_review(report['matches'], approvals):
        sys.exit('The complete scan differs from the approved release findings')
    for match in blocked:
        v, package = match['vulnerability'], match['artifact']
        print(f"{v['id']}: {package['name']} {package['version']} -> {', '.join(v['fix'].get('versions', []))}")
    print(f"{len(report['matches'])} findings retained; {len(blocked)} blocking findings")
    sys.exit(bool(blocked))
