#!/usr/bin/env python3
"""Create synthetic upgrade-test appdata with the published Beta 3 image.

Usage: python3 scripts/make-upgrade-fixture.py /tmp/antennarr-upgrade-fixture
Requires Docker. Refuses an existing output directory. Never reads live appdata.
"""
import argparse
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('output', type=Path)
parser.add_argument('--image', default='antennarr/antennarr@sha256:c9440b4f3670def133a7d3b95c003a840dbea44a065a0b92668b8882d57f7951')
args = parser.parse_args()
root = args.output.resolve()
if root.exists():
    parser.error('Output already exists; choose a new disposable directory')
root.mkdir(parents=True, mode=0o755)
data = root / 'data'
data.mkdir(mode=0o700)
uid, gid = (10001, 10001) if os.geteuid() == 0 else (os.getuid(), os.getgid())
if os.geteuid() == 0:
    os.chown(data, uid, gid)
name = f'antennarr-upgrade-fixture-{os.getpid()}'


def docker(*parts):
    return subprocess.check_output(['docker', *parts], stderr=subprocess.PIPE)


try:
    docker('run', '-d', '--name', name, '--network', 'none', '--user', f'{uid}:{gid}',
           '--mount', f'type=bind,src={data},dst=/data',
           '-e', 'ANTENNARR_ADMIN_LISTEN=:9193', '-e', 'ANTENNARR_BASE_URL=http://fixture:9192', args.image)

    def get(path, admin=True):
        auth = ['-u', 'admin:admin'] if admin else []
        port = '9193' if admin else '9192'
        return docker('exec', name, 'curl', '-fsS', *auth, f'http://127.0.0.1:{port}{path}').decode()

    for attempt in range(40):
        try:
            get('/api/settings')
            break
        except subprocess.CalledProcessError:
            time.sleep(0.2)
    else:
        raise RuntimeError('Published image failed to initialise')
    channels = json.loads(get('/api/channels'))
    channel = next(c['id'] for c in channels if c['name'] == 'CNN')
    expected = {path: get(path, False) for path in ['/discover.json', '/lineup.json', '/device.xml']}
    expected.update(base='http://fixture:9192', channel=channel)
    docker('stop', name)
    with sqlite3.connect(data / 'database/antennarr.db') as db:
        assert 'last_failure_kind' not in [row[1] for row in db.execute('pragma table_info(accounts)')]
        stamp = int(time.time())
        for number in range(1, 32):
            host = f'fixture{number}.invalid'
            secret = json.dumps({'kind': 'direct', 'url': f'https://{host}/stream.ts', 'endpoint': f'https://{host}', 'name': 'CNN'})
            db.execute("insert into accounts(id,identity,provider,kind,secret,state,created,rate_key,catalogue_checked) values(?,?,?,'direct',?,'healthy',?,?,?)",
                       (number, f'account-{number}', host, secret, stamp, host, stamp))
            db.execute("insert into streams(id,account_id,identity,name,secret,state,last_success) values(?,?,?,'CNN',?,'healthy',?)",
                       (number, number, f'stream-{number}', secret, stamp))
            db.execute("insert into mappings values(?,?,1,'automatic')", (channel, number))
            if number <= 30:
                db.execute('insert into roster(channel_id,stream_id,account_id,admitted,verified,due) values(?,?,?,?,?,?)',
                           (channel, number, number, stamp, stamp, stamp))
    (root / 'expected.json').write_text(json.dumps(expected))
    print('Synthetic Beta 3 fixture ready: 31 accounts, 30 roster entries, 143 channels')
finally:
    subprocess.run(['docker', 'rm', '-f', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
