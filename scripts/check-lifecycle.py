#!/usr/bin/env python3
"""Exercise the packaged engine as two HTTP clients and a real owned process."""
import hashlib,json,os,pathlib,subprocess,tempfile,time,urllib.request,urllib.error
engine=pathlib.Path(__file__).resolve().parent.parent/'build/NearTransfer.app/Contents/Resources/neartransfer-engine'
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
def request(base,token,path,body=None,headers=None):
    h={'Authorization':'Bearer '+token};h.update(headers or {})
    with opener.open(urllib.request.Request(base+path,data=body,headers=h),timeout=5) as r:return r.status,r.read()
def start(storage,qr):
    p=subprocess.Popen([str(engine),str(storage),str(qr)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    try: cfg=json.loads(p.stdout.readline())
    except BaseException:p.terminate();p.wait(timeout=5);raise
    return p,cfg
with tempfile.TemporaryDirectory(prefix='neartransfer-lifecycle-') as temp:
    root=pathlib.Path(temp);data=root/'Inbox';qr=root/'QR';qr.mkdir()
    p,c=start(data,qr)
    try:
        other=subprocess.run([str(engine),str(data),str(qr)],input=b'',capture_output=True,timeout=5)
        assert other.returncode!=0 and '另一个近传实例'.encode() in other.stderr
        expected=[]
        for origin in ['mac','browser']:
            payload=(origin.encode()+bytes(range(256)))*8192
            code,raw=request(c['local'],c['token'],'/items',payload,{'X-Origin':origin,'X-File-Name':'same.dat','Content-Type':'application/octet-stream'})
            item=json.loads(raw);assert code==201
            code,download=request(c['local'],c['token'],'/download/'+item['id'],urllib.parse.urlencode({'token':c['token']}).encode(),{'Content-Type':'application/x-www-form-urlencoded','Origin':c['local']})
            assert code==200 and hashlib.sha256(download).digest()==hashlib.sha256(payload).digest()
            expected.append(item)
        old=c
        p.stdin.close();p.wait(timeout=5);assert p.returncode==0
        try:opener.open(old['local'],timeout=1);raise AssertionError('listener survived parent EOF')
        except urllib.error.URLError:pass
        p,c=start(data,qr)
        assert c['token']!=old['token'] and c['admin']!=old['admin']
        assert json.loads(request(c['local'],c['token'],'/items')[1])==expected
        try:request(c['local'],old['token'],'/items');raise AssertionError('old credential accepted')
        except urllib.error.HTTPError as e:assert e.code==401
        p.terminate();p.wait(timeout=5);assert p.returncode==0
        print('PASS: bidirectional binary downloads match SHA-256; storage lock; stdin EOF; SIGTERM; restart persistence; credential rotation')
    finally:
        if p.poll() is None:p.kill();p.wait()
