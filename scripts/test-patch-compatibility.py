#!/usr/bin/env python3
"""Exercise two same-minor server binaries against one disposable installation."""
import argparse
import base64
from contextlib import contextmanager, ExitStack
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import time
from urllib.error import URLError
from urllib.request import build_opener, ProxyHandler, Request


def check(actual, expected, label):
    if actual != expected:
        raise RuntimeError(f'{label} changed: expected {expected!r}, got {actual!r}')


class Installation:
    def __init__(self, root):
        # Reserve all three ports together so they are distinct, even on a busy runner.
        with ExitStack() as leases:
            ports = []
            for _ in range(3):
                sock = leases.enter_context(socket.socket())
                sock.bind(('127.0.0.1', 0))
                ports.append(sock.getsockname()[1])
        self.admin = f'http://127.0.0.1:{ports[0]}'
        self.tuner = f'http://127.0.0.1:{ports[1]}'
        self.login = 'admin:admin'
        self.http = build_opener(ProxyHandler({}))
        self.env = {k: v for k, v in os.environ.items() if not k.startswith('ANTENNARR_')}
        self.env.update(ANTENNARR_DATA=str(root), ANTENNARR_BASE_URL=self.tuner,
                        ANTENNARR_ADMIN_LISTEN=f'127.0.0.1:{ports[0]}',
                        ANTENNARR_LISTEN=f'127.0.0.1:{ports[1]}', ANTENNARR_TUNER_PORTS=str(ports[2]))
        if ffmpeg := os.environ.get('ANTENNARR_FFMPEG') or shutil.which('ffmpeg'):
            self.env['ANTENNARR_FFMPEG'] = ffmpeg

    def request(self, path, data=None, *, content_type='application/json', raw=False):
        headers = {'X-Antennarr-Request': '1', 'Content-Type': content_type,
                   'Authorization': 'Basic ' + base64.b64encode(self.login.encode()).decode()}
        if data is not None and not isinstance(data, bytes):
            data = json.dumps(data).encode()
        with self.http.open(Request(self.admin + path, data=data, headers=headers), timeout=10) as r:
            return r.read() if raw else json.load(r)

    @contextmanager
    def running(self, binary):
        with tempfile.TemporaryFile() as log:
            proc = subprocess.Popen([binary], env=self.env, stdin=subprocess.DEVNULL,
                                    stdout=log, stderr=subprocess.STDOUT)
            try:
                until = time.monotonic() + 20
                while proc.poll() is None and time.monotonic() < until:
                    try:
                        check(self.request('/api/connection')['tuner_url'], self.tuner, 'test listener')
                        break
                    except URLError:
                        time.sleep(0.1)
                else:
                    log.seek(0)
                    raise RuntimeError(f'{binary} failed to serve the test installation: {log.read()[-2000:]!r}')
                yield
            finally:
                if proc.poll() is None:
                    proc.terminate()
                    try:
                        proc.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        proc.kill()
                        proc.wait()
                        raise RuntimeError(f'{binary} did not stop cleanly')

    def restore(self, name, archive):
        boundary = secrets.token_hex(24)
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="backup"; '
                f'filename="{name}"\r\nContent-Type: application/zip\r\n\r\n').encode()
        result = self.request('/api/restore', body + archive + f'\r\n--{boundary}--\r\n'.encode(),
                              content_type='multipart/form-data; boundary=' + boundary)
        check(result['restoring'], True, 'restore request')

    def snapshot(self):
        profiles = self.request('/api/profiles')
        check(len(profiles), 1, 'tuner count')
        p = profiles[0]
        with self.http.open(self.tuner + '/discover.json', timeout=10) as response:
            device = json.load(response)
        with self.http.open(self.tuner + '/lineup.json', timeout=10) as response:
            lineup = json.load(response)
        return {'identity': [p[k] for k in ('id', 'preset_id', 'device_id', 'name', 'port_offset',
                                             'enabled', 'guide_location', 'tuner_url')],
                'plex': [device[k] for k in ('DeviceID', 'BaseURL', 'LineupURL')],
                'advertised': lineup, 'channels': self.request('/api/lineup?profile_id=' + p['id']),
                'settings': self.request('/api/settings')}

    def idle_pass(self):
        # The first idle checker pass reconciles rosters and runs catalogue housekeeping.
        until = time.monotonic() + 15
        while time.monotonic() < until:
            state = self.request('/api/monitor')['checker']
            if state['state'] in ('waiting_candidates', 'checks_waiting', 'rosters_full'):
                if state.get('last_error'):
                    raise RuntimeError(f'Checker failed: {state}')
                return
            time.sleep(0.1)
        raise RuntimeError('Idle checker did not finish a pass')


def exercise(baseline, candidate, app):
    with app.running(baseline):
        check(app.request('/api/profiles'), [], 'fresh installation')
        profile = app.request('/api/profiles', {'preset_id': 'us-nyc'})['id']
        app.request('/api/profiles/' + profile, {'enabled': True, 'name': 'Compatibility tuner'})
        app.request('/api/settings', {'roster_target': 12})
        password = secrets.token_hex(18)
        app.request('/api/login', {'current_password': 'admin', 'username': 'tester', 'password': password})
        app.login = 'tester:' + password
        channel = dict(profile_id=profile, number='9002', name='Fixture local', region='US',
                       station='WTEST', enabled=True, aliases=['Fixture local', '-timeshift'])
        channel['id'] = app.request('/api/channels', channel)['id']
        before = app.snapshot()

    with app.running(candidate):
        state = app.snapshot()
        # New patch versions may expose new fields. Check every baseline value.
        check({k: state[k] for k in ('identity', 'plex', 'advertised')},
              {k: before[k] for k in ('identity', 'plex', 'advertised')}, 'upgraded tuner')
        for key, value in before['settings'].items():
            check(state['settings'][key], value, key)
        old_channels = {c['id']: c for c in before['channels']['channels']}
        new_channels = state['channels']['channels']
        check({c['id'] for c in new_channels}, set(old_channels), 'channel IDs')
        for current in new_channels:
            for key, value in old_channels[current['id']].items():
                check(current[key], value, f'{current["id"]}.{key}')
        app.request('/api/settings', {'roster_target': 13})
        channel.update(aliases=['Fixture local', '-timeshift', 'Fixture HD'])
        app.request('/api/channels', channel)
        expected = app.snapshot()

    with app.running(baseline):
        check(app.request('/api/settings')['roster_target'], 13, 'candidate setting on downgrade')
        saved = next(c for c in app.request('/api/channels') if c['id'] == channel['id'])
        check(saved['aliases'], channel['aliases'], 'candidate alias on downgrade')
        app.request('/api/settings', {'roster_target': 14})
        # The earlier version writes its own representation of the saved channel.
        saved['name'] = 'Older writer local'
        app.request('/api/channels', saved)
        app.idle_pass()
        backup = app.request('/api/backup', {})
        archive = app.request(backup['download'], raw=True)
        app.request('/api/settings', {'roster_target': 15})
        app.restore(backup['backup'], archive)

    with app.running(candidate):
        expected['settings']['roster_target'] = 14
        next(c for c in expected['channels']['channels'] if c['id'] == channel['id'])['name'] = saved['name']
        next(c for c in expected['advertised'] if c['GuideNumber'] == channel['number'])['GuideName'] = saved['name']
        app.idle_pass()
        check(app.snapshot(), expected, 're-upgrade after older writer and restore')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('baseline', type=Path, help='Earlier same-minor server executable')
    parser.add_argument('candidate', type=Path, help='Candidate server executable')
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='antennarr-patch-') as directory:
        exercise(args.baseline.resolve(), args.candidate.resolve(), Installation(Path(directory)))
    print('Passed baseline → candidate → older writer and backup/restore → candidate.')
