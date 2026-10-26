#!/usr/bin/env python3
"""Fixed-source local build runner. No Docker socket is mounted into Mini Fury and no arbitrary commands or repositories are accepted."""
import argparse,hashlib,hmac,json,subprocess,time,urllib.request,urllib.error
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def source_revision():
    digest=hashlib.sha256()
    for name in ['Dockerfile','main.go']:
        digest.update(name.encode()+b'\0'+(ROOT/'infra/kubernetes/lab'/name).read_bytes())
    return digest.hexdigest()
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--target',choices=['main','test'],default='test');parser.add_argument('--once',action='store_true');parser.add_argument('--print-revision',action='store_true');args=parser.parse_args()
    if args.print_revision:print(source_revision());return
    directory=ROOT/'.local/kube-lab'/args.target;config=json.loads((directory/'lab.json').read_text());key=(directory/'agent-token').read_text().strip();base='http://127.0.0.1:'+('18080' if args.target=='test' else '8080')
    def call(path,body=None,signed=False):
        data=None if body is None else json.dumps(body,separators=(',',':')).encode();headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'}
        if signed:headers['X-Mini-Fury-Signature']='sha256='+hmac.new(key.encode(),data,hashlib.sha256).hexdigest()
        req=urllib.request.Request(base+path,data=data,headers=headers)
        with urllib.request.urlopen(req,timeout=10) as response:
            return None if response.status==204 else json.load(response)
    while True:
        job=call('/api/runtime/build-agent/claim',{})
        if not job:
            if args.once:return
            time.sleep(3);continue
        item=job['job'];payload=job['payload'];lease=job['leaseToken'];valid=item['applicationId']==config['applicationId'] and payload['sourceRevision']==source_revision() and payload['provider']=='local'
        image='';success=False
        if valid:
            tag='127.0.0.1:15050/mini-fury/lab-app/api:'+item['id']
            logpath=directory/'last-build.log'
            def command(cmd):
                with logpath.open('ab') as output:
                    process=subprocess.Popen(cmd,cwd=ROOT,stdout=output,stderr=subprocess.STDOUT)
                    deadline=time.monotonic()+180;next_heartbeat=0
                    while process.poll() is None:
                        if time.monotonic()>deadline:process.terminate();process.wait(timeout=10);raise RuntimeError('local build timed out')
                        if time.monotonic()>=next_heartbeat:
                            try:call('/api/runtime/build-agent/jobs/'+item['id']+'/heartbeat',{'leaseToken':lease})
                            except Exception:process.terminate();process.wait(timeout=10);raise RuntimeError('build lease cancelled')
                            next_heartbeat=time.monotonic()+8
                        time.sleep(1)
                    if process.returncode:raise RuntimeError('fixed lab build command failed')
            try:
                command(['docker','build','--build-arg','BUILD_LABEL='+payload['name'],'--build-arg','FAIL_READINESS='+('true' if payload['name'].startswith('broken-') else 'false'),'-t',tag,'infra/kubernetes/lab'])
                command(['docker','push',tag])
                info=json.loads(subprocess.check_output(['docker','image','inspect',tag],cwd=ROOT))[0]
                digest=next(v.split('@',1)[1] for v in info['RepoDigests'] if v.startswith('127.0.0.1:15050/'))
                image=config['imageRepository']+'@'+digest;success=True
            except Exception:success=False
        report={'leaseToken':lease,'sourceRevision':payload['sourceRevision'],'image':image,'success':success}
        call('/api/runtime/build-agent/jobs/'+item['id']+'/complete',report,True)
        print('Local build report:',item['id'],'succeeded' if success else 'failed')
        if args.once:return
if __name__=='__main__':main()
