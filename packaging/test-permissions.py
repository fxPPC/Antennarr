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
try:
 start();ready();identity(10001);channels=json.loads(request('/api/channels').stdout);assert len(channels)==143
 accounts=json.loads(request('/api/input/accounts').stdout);assert accounts['items']==[] and accounts['total']==0;stop();print('PASS default IDs, 143 channels, zero accounts',flush=True)
 start(puid=12345,pgid=12346);ready();identity(12345);assert json.loads(request('/api/channels').stdout)==channels;stop();print('PASS custom PUID/PGID adjusts existing ownership and preserves channels',flush=True)
 start(uid='12345:12346',extra=('--cap-drop','ALL','--read-only','--tmpfs','/tmp'));ready();identity(12345);stop();print('PASS explicit --user and no capabilities',flush=True)
 start(puid=12345,pgid=12346,extra=('--cap-drop','ALL','--cap-add','CHOWN','--cap-add','DAC_OVERRIDE','--cap-add','SETUID','--cap-add','SETGID','--read-only','--tmpfs','/tmp','--security-opt','no-new-privileges:true'));ready();identity(12345);stop();print('PASS hardened initialisation',flush=True)
 start(uid='22222:22222');time.sleep(.5);logs=run('logs',name).stdout+run('logs',name).stderr;assert 'permission denied' in logs,logs;stop();print('PASS wrong ownership diagnostic',flush=True)
 start(uid='12345:12346',puid=5);time.sleep(.5);logs=run('logs',name).stdout+run('logs',name).stderr;assert 'differ from Docker --user' in logs,logs;stop();print('PASS conflicting IDs rejected',flush=True)
 start(puid='not-a-number');time.sleep(.5);logs=run('logs',name).stdout+run('logs',name).stderr;assert 'positive numeric' in logs,logs;stop();print('PASS invalid IDs rejected',flush=True)
finally:
 stop();run('volume','rm',volume,ok=False)
