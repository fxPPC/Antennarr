#!/usr/bin/env python3
"""Create synthetic upgrade-test appdata from pinned release source.

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
parser.add_argument('--baseline', choices=['v1.0.0', 'v1.0.4', 'v1.0.9'], default='v1.0.0')
parser.add_argument('--image', help='Use an existing image built from the selected baseline')
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
    image = args.image
    if not image:
        revision = {
            'v1.0.0': '4f948cca0f4395bbe8aad109558e1fe151775827',
            'v1.0.9': '74e7fc2ea3ad88c72d503bd173a02189322920f0',
            'v1.0.4': 'e1fdf4bc738e0a39c1b83a9f1f3c8148bb6758f4',
        }[args.baseline]
        image = f'antennarr-fixture:{revision}'
        subprocess.run(['docker', 'build', '--tag', image,
                        f'https://github.com/fxPPC/Antennarr.git#{revision}'], check=True)
    docker('run', '-d', '--name', name, '--network', 'none', '--user', f'{uid}:{gid}',
           '--mount', f'type=bind,src={data},dst=/data',
           '-e', 'ANTENNARR_ADMIN_LISTEN=:9193', '-e', 'ANTENNARR_BASE_URL=http://fixture:9192', image)

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
        raise RuntimeError('Baseline image failed to initialise')
    channels = json.loads(get('/api/channels'))
    channel = next(c['id'] for c in channels if c['name'] == 'CNN')
    if args.baseline != 'v1.0.0':
        def save_channel(value):
            docker('exec', name, 'curl', '-fsS', '-u', 'admin:admin',
                   '-H', 'X-Antennarr-Request: 1', '-H', 'Content-Type: application/json',
                   '--data-binary', json.dumps(value), 'http://127.0.0.1:9193/api/channels')

        # Exercise existing user edits as well as the published preset. These
        # must survive migration into the first optional tuner later in 1.1.
        edited = next(c for c in channels if c['name'] == 'NBC')
        edited = {key: edited[key] for key in ('id', 'number', 'name', 'region', 'enabled', 'aliases', 'station')}
        edited.update(number='9001', name='Fixture NBC', aliases=edited['aliases'] + ['Fixture NBC'])
        save_channel(edited)
        save_channel(dict(number='9002', name='Fixture Local', region='US',
                          station='WTEST', enabled=False, aliases=['Fixture Local']))
        channels = json.loads(get('/api/channels'))
    expected = {path: get(path, False) for path in ['/discover.json', '/lineup.json', '/device.xml']}
    settings = json.loads(get('/api/settings'))
    expected.update(replenish=str(settings.get('replenish_while_playing', False)).lower(), base='http://fixture:9192', channel=channel, channel_count=str(len(channels)), baseline=args.baseline)
    if args.baseline != 'v1.0.0':
        expected['lineup_export'] = get('/api/lineup')
    docker('stop', name)
    with sqlite3.connect(data / 'database/antennarr.db') as db:
        has_diagnostics = 'last_failure_kind' in [row[1] for row in db.execute('pragma table_info(accounts)')]
        assert has_diagnostics, 'Unexpected published baseline schema'
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
    print(f'Synthetic {args.baseline} fixture ready: 31 accounts, 30 roster entries, {len(channels)} channels')
finally:
    subprocess.run(['docker', 'rm', '-f', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
