# Run only against disposable test appdata; creates and removes its own volume.
import subprocess,json,time,os
image=os.environ.get('ANTENNARR_TEST_IMAGE','antennarr:permissions-test'); volume='antennarr-permissions-test-data'; name='antennarr-permissions-smoke'
def run(*args,ok=True):
 p=subprocess.run(['sudo','docker',*args],capture_output=True,text=True)
 if ok and p.returncode:raise RuntimeError(p.stderr+p.stdout)
 return p

def start(uid=None,puid=None,pgid=None,vol=volume,extra=()):
 args=['run','-d','--name',name,'--network','none','-e','ANTENNARR_ADMIN_LISTEN=:9193','-e','ANTENNARR_BASE_URL=http://antennarr:9192','--mount','type=volume,src='+vol+',dst=/data',*extra]
 if uid:args+=['--user',uid]
 if puid is not None:args+=['-e','PUID='+str(puid)]
 if pgid is not None:args+=['-e','PGID='+str(pgid)]
 run(*args,image)
def request(path):return run('exec',name,'curl','-fsS','-u','admin:admin','http://127.0.0.1:9193'+path)
def update(path,payload):return run('exec',name,'curl','-fsS','-u','admin:admin','-H','X-Antennarr-Request: 1','-H','Content-Type: application/json','-d',json.dumps(payload),'http://127.0.0.1:9193'+path)
def ready():
 for _ in range(30):
  p=run('exec',name,'curl','-fsS','-u','admin:admin','http://127.0.0.1:9193/api/login',ok=False)
  if p.returncode==0:return
  time.sleep(.2)
 raise RuntimeError(run('logs',name).stdout+run('logs',name).stderr)
def stop():run('rm','-f',name,ok=False)
def identity(want):
 out=run('exec',name,'cat','/proc/1/status').stdout
 assert next(x for x in out.splitlines() if x.startswith('Uid:')).split()[1]==str(want),out
def test_range(first, explicit):
    args = ['run', '-d', '--name', name, '--network', 'none',
            '--tmpfs', '/data', '-e', 'ANTENNARR_BASE_URL=http://fixture']
    if explicit:
        args += ['-e', f'ANTENNARR_PORT_RANGE={first}-{first + 6}']
    run(*args, image)
    try:
        for _ in range(40):
            if run('exec', name, 'antennarr', 'healthcheck', ok=False).returncode == 0:
                break
            time.sleep(0.25)
        else:
            raise RuntimeError('Range-mode Docker health check did not become ready')
        result = run('exec', name, 'curl', '-fsS', '-u', 'admin:admin',
                     f'http://127.0.0.1:{first}/api/listeners')
        assert json.loads(result.stdout) == {
            'admin_port': first, 'port_range': f'{first}-{first + 6}',
            'tuner_ports': list(range(first + 1, first + 7)), 'available_slots': 6,
        }
        for port in range(first + 1, first + 7):
            response = run('exec', name, 'curl', '-s', '-o', '/dev/null', '-w', '%{http_code}',
                           '-u', 'admin:admin', f'http://127.0.0.1:{port}/api/status')
            assert response.stdout == '404', (port, response.stdout)
    finally:
        stop()

try:
 start();ready();identity(10001);assert not json.loads(request('/api/channels').stdout);assert not json.loads(request('/api/profiles').stdout)
 for asset in ('/admin/ui/setup.js', '/admin/ui/source-import.js'):
  assert request(asset).stdout.strip(), asset
 setup=json.loads(request('/api/setup').stdout)
 assert setup['state']=='pending' and not setup['has_tuner'] and not setup['has_sources'], setup
 update('/api/setup',{'state':'active'})
 expected={'us-nyc':149,'uk-london':109,'mx-mexico-city':15,'in-mumbai':23,'de-berlin':34,'nl-amsterdam':22}
 for preset in expected:
  update('/api/profiles',{'preset_id':preset})
 channels=json.loads(request('/api/channels').stdout)
 assert len(channels)==sum(expected.values())
 profiles=json.loads(request('/api/profiles').stdout)
 assert {p['preset_id']:p['channel_count'] for p in profiles}==expected and not any(p['enabled'] for p in profiles)
 for profile in profiles:
  update('/api/profiles/'+profile['id'],{'enabled':True})
  url='http://127.0.0.1:'+str(profile['listen_port'])+'/lineup.json'
  lineup=json.loads(run('exec',name,'curl','-fsS',url).stdout)
  assert len(lineup)==expected[profile['preset_id']]
 setup=json.loads(request('/api/setup').stdout)
 assert setup['state']=='active' and setup['has_tuner'] and not setup['has_sources'], setup
 update('/api/setup',{'state':'dismissed'})
 accounts=json.loads(request('/api/input/accounts').stdout);assert accounts['items']==[] and accounts['total']==0;stop();print('PASS default IDs, six optional tuners and scoped lineups, zero accounts',flush=True)
 start(puid=12345,pgid=12346);ready();identity(12345);assert json.loads(request('/api/channels').stdout)==channels;assert json.loads(request('/api/setup').stdout)['state']=='dismissed';stop();print('PASS custom PUID/PGID adjusts existing ownership and preserves channels and wizard dismissal',flush=True)
 start(uid='12345:12346',extra=('--cap-drop','ALL','--read-only','--tmpfs','/tmp'));ready();identity(12345);stop();print('PASS explicit --user and no capabilities',flush=True)
 start(puid=12345,pgid=12346,extra=('--cap-drop','ALL','--cap-add','CHOWN','--cap-add','DAC_OVERRIDE','--cap-add','SETUID','--cap-add','SETGID','--read-only','--tmpfs','/tmp','--security-opt','no-new-privileges:true'));ready();identity(12345);stop();print('PASS hardened initialisation',flush=True)
 start(uid='22222:22222');time.sleep(.5);logs=run('logs',name).stdout+run('logs',name).stderr;assert 'permission denied' in logs,logs;stop();print('PASS wrong ownership diagnostic',flush=True)
 start(uid='12345:12346',puid=5);time.sleep(.5);logs=run('logs',name).stdout+run('logs',name).stderr;assert 'differ from Docker --user' in logs,logs;stop();print('PASS conflicting IDs rejected',flush=True)
 start(puid='not-a-number');time.sleep(.5);logs=run('logs',name).stdout+run('logs',name).stderr;assert 'positive numeric' in logs,logs;stop();print('PASS invalid IDs rejected',flush=True)
 test_range(39192, False)
 test_range(41000, True)
 print('PASS fresh and custom Docker port ranges, admin isolation, and health checks',flush=True)
finally:
 stop();run('volume','rm',volume,ok=False)
