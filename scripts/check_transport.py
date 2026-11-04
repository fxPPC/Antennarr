#!/usr/bin/env python3
"""Capture generated tuner media, check TS counters, and decode it. No secrets required."""
import argparse, concurrent.futures, json, pathlib, subprocess, tempfile, time, urllib.request

def check(base,number,duration):
    begin=time.monotonic(); data=bytearray()
    with urllib.request.urlopen(base+'/auto/v'+number,timeout=20) as r:
        first=None
        while time.monotonic()-begin<duration:
            b=r.read(1316)
            if not b: break
            if first is None:first=time.monotonic()-begin
            data.extend(b)
    assert len(data)>188*100, 'insufficient media'
    assert len(data)%188==0, 'partial TS packet'
    counters={};errors=0;pids=set()
    for i in range(0,len(data),188):
        p=data[i:i+188];assert p[0]==0x47,'lost TS sync'
        assert not p[1]&0x80,'transport error indicator'
        pid=((p[1]&31)<<8)|p[2];pids.add(pid)
        afc=(p[3]>>4)&3;cc=p[3]&15
        assert afc!=0,'reserved adaptation control'
        disc=bool(afc&2 and p[4]>0 and p[5]&0x80)
        if pid!=8191 and afc&1:
            if pid in counters and not disc and cc!=(counters[pid]+1)%16:errors+=1
            counters[pid]=cc
    assert 0 in pids,'no PAT'
    with tempfile.TemporaryDirectory(prefix='antennarr-media-') as d:
        path=pathlib.Path(d)/'capture.ts';path.write_bytes(data)
        probe=subprocess.run(['ffprobe','-v','error','-show_streams','-show_packets','-show_entries','stream=codec_name,codec_type,width,height:packet=stream_index,dts_time','-of','json',str(path)],capture_output=True,check=True)
        info=json.loads(probe.stdout);last={};backwards=0
        for p in info['packets']:
            if 'dts_time' not in p:continue
            idx=p['stream_index'];ts=float(p['dts_time'])
            if idx in last and ts<last[idx]:backwards+=1
            last[idx]=ts
        dec=subprocess.run(['ffmpeg','-v','error','-i',str(path),'-t',str(max(1,duration-4)),'-f','null','-'],capture_output=True)
        assert dec.returncode==0 and not dec.stderr, 'decoder errors: '+dec.stderr[:300].decode(errors='replace')
    assert errors==0, f'{errors} continuity errors'
    assert backwards==0, f'{backwards} timestamp regressions'
    return dict(channel=number,bytes=len(data),first_media_seconds=round(first,3),duration_seconds=round(time.monotonic()-begin,2),continuity_errors=errors,dts_regressions=backwards,streams=info['streams'])
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--base',default='http://127.0.0.1:49192');p.add_argument('--seconds',type=int,default=20);a=p.parse_args()
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as ex:
        results=list(ex.map(lambda n:check(a.base,n,a.seconds),['901','902','903']))
    print(json.dumps(results,indent=2))
