#!/usr/bin/env python3
"""Create only the explicitly named Mini Fury Docker/kind laboratory; never select a global kube context."""
import argparse,base64,hashlib,json,os,platform,secrets,subprocess,urllib.request
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
LAB=ROOT/'.local/kube-lab'
KIND_VERSION='v0.33.0'
NODE='kindest/node:v1.36.4@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed'
def run(args,**kwargs):return subprocess.run(args,cwd=ROOT,check=True,**kwargs)
def private(path,data):
    path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
    with os.fdopen(fd,'wb') as f:f.write(data if isinstance(data,bytes) else data.encode())
def tool():
    system={'Darwin':'darwin','Linux':'linux'}[platform.system()];arch={'arm64':'arm64','aarch64':'arm64','x86_64':'amd64'}[platform.machine()]
    path=ROOT/'.local/tools/kind';path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    if path.exists():return str(path)
    name='kind-'+system+'-'+arch;base='https://github.com/kubernetes-sigs/kind/releases/download/'+KIND_VERSION+'/'
    with urllib.request.urlopen(base+name,timeout=60) as r:data=r.read()
    with urllib.request.urlopen(base+name+'.sha256sum',timeout=20) as r:expected=r.read().decode().split()[0]
    if hashlib.sha256(data).hexdigest()!=expected:raise ValueError('kind checksum mismatch')
    private(path,data);path.chmod(0o700);return str(path)
def kube(*args,**kwargs):return run(['kubectl','--kubeconfig',str(LAB/'admin-kubeconfig'),'--context','kind-mini-fury-lab',*args],**kwargs)
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--application-id',required=True);parser.add_argument('--target',default='main',choices=['main','test']);args=parser.parse_args()
    import re
    if not re.fullmatch(r'[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}',args.application_id):raise ValueError('invalid application id')
    LAB.mkdir(parents=True,exist_ok=True,mode=0o700);kind=tool()
    names=run([kind,'get','clusters'],capture_output=True,text=True).stdout.splitlines()
    if 'mini-fury-lab' not in names:run([kind,'create','cluster','--name','mini-fury-lab','--kubeconfig',str(LAB/'admin-kubeconfig'),'--image',NODE,'--config','infra/kubernetes/lab/kind.yml','--wait','120s'])
    elif not (LAB/'admin-kubeconfig').exists():run([kind,'get','kubeconfig','--name','mini-fury-lab'],stdout=(LAB/'admin-kubeconfig').open('w'))
    os.chmod(LAB/'admin-kubeconfig',0o600)
    registry='mini-fury-lab-registry'
    containers=run(['docker','ps','-a','--format','{{.Names}}'],capture_output=True,text=True).stdout.splitlines()
    if registry not in containers:run(['docker','run','-d','--name',registry,'--restart','unless-stopped','-p','127.0.0.1:15050:5000','-v','mini-fury-lab-registry:/var/lib/registry','registry:3.1.2'])
    else:run(['docker','start',registry],stdout=subprocess.DEVNULL)
    peers=json.loads(run(['docker','network','inspect','kind'],capture_output=True,text=True).stdout)[0]['Containers']
    if registry not in [v['Name'] for v in peers.values()]:run(['docker','network','connect','kind',registry])
    hosts='server = "http://mini-fury-lab-registry:5000"\n[host."http://mini-fury-lab-registry:5000"]\n  capabilities = ["pull", "resolve"]\n'
    run(['docker','exec','mini-fury-lab-control-plane','mkdir','-p','/etc/containerd/certs.d/mini-fury-lab-registry:5000'])
    run(['docker','exec','-i','mini-fury-lab-control-plane','sh','-c','cat > /etc/containerd/certs.d/mini-fury-lab-registry:5000/hosts.toml'],input=hosts,text=True)
    namespace='mf-'+args.target+'-'+args.application_id[:8]
    resources=[{'apiVersion':'v1','kind':'Namespace','metadata':{'name':namespace,'labels':{'mini-fury.io/laboratory':'true'}}},
       {'apiVersion':'v1','kind':'ServiceAccount','metadata':{'name':'mini-fury','namespace':namespace}},
       {'apiVersion':'rbac.authorization.k8s.io/v1','kind':'Role','metadata':{'name':'mini-fury','namespace':namespace},'rules':[
         {'apiGroups':[''],'resources':['pods','events'],'verbs':['get','list']},
         {'apiGroups':[''],'resources':['pods/log'],'verbs':['get']},
         *([{'apiGroups':[''],'resources':['pods/proxy'],'verbs':['get']},{'apiGroups':['apps'],'resources':['statefulsets'],'verbs':['get','create','update']}] if args.target=='test' else []),
         {'apiGroups':[''],'resources':['services'],'verbs':['get','create','update']},
         {'apiGroups':['apps'],'resources':['deployments'],'verbs':['get','create','update']},
         {'apiGroups':['apps'],'resources':['replicasets'],'verbs':['get','list']},
         {'apiGroups':['metrics.k8s.io'],'resources':['pods'],'verbs':['get','list']}]},
       {'apiVersion':'rbac.authorization.k8s.io/v1','kind':'RoleBinding','metadata':{'name':'mini-fury','namespace':namespace},'subjects':[{'kind':'ServiceAccount','name':'mini-fury','namespace':namespace}],'roleRef':{'apiGroup':'rbac.authorization.k8s.io','kind':'Role','name':'mini-fury'}}]
    kube('apply','-f','-',input=json.dumps({'apiVersion':'v1','kind':'List','items':resources}),text=True)
    directory=LAB/args.target;directory.mkdir(exist_ok=True,mode=0o700)
    data=json.loads(kube('config','view','--raw','-o','json',capture_output=True,text=True).stdout)
    ca=base64.b64decode(data['clusters'][0]['cluster']['certificate-authority-data'])
    token=kube('create','token','mini-fury','-n',namespace,'--duration=24h',capture_output=True,text=True).stdout.strip()
    private(directory/'ca.crt',ca);private(directory/'token',token)
    if not (directory/'agent-token').exists():private(directory/'agent-token',secrets.token_urlsafe(32))
    server='https://mini-fury-lab-control-plane:6443'
    config={'clusters':[{'id':'local-lab','name':'Laboratorio local aislado','server':server,'caFile':'/run/mini-fury-runtime/ca.crt','tokenFile':'/run/mini-fury-runtime/token','bindings':[{'namespace':namespace,'applicationId':args.application_id,'secrets':{'lab-message':{'name':'lab-message','key':'message'}}}]}],
     'repositories':[{'applicationId':args.application_id,'provider':'local','repository':'https://example.invalid/mini-fury/lab-app','ref':'local','agentId':'lab','imagePrefixes':['mini-fury-lab-registry:5000/mini-fury/lab-app/']}],
     'agents':[{'id':'lab','tokenFile':'/run/mini-fury-runtime/agent-token'}]}
    if args.target=='main':config['repositories']=[];config['agents']=[]
    if args.target=='test':
        profiles=[]
        for engine,image,alias in [('postgres','postgres:17-alpine','postgres-password'),('redis','redis:8.2-alpine','redis-configuration')]:
            run(['docker','pull',image],stdout=subprocess.DEVNULL)
            info=json.loads(run(['docker','image','inspect',image],capture_output=True,text=True).stdout)[0]
            digest=info['RepoDigests'][0].split('@',1)[1]
            profiles.append({'id':engine+'-dev','name':engine.title()+' de desarrollo','engine':engine,'image':('docker.io/library/'+engine+'@'+digest),'secretAlias':alias,'cpuMilli':250,'memoryMiB':256,'storageClass':'standard'})
            secret_name='managed-'+engine
            if engine=='postgres': key='password';value=secrets.token_urlsafe(32)
            else:key='redis.conf';value='bind 0.0.0.0\nprotected-mode yes\ndir /data\nrequirepass '+secrets.token_urlsafe(32)+'\n'
            config['clusters'][0]['bindings'][0]['secrets'][alias]={'name':secret_name,'key':key}
            found=subprocess.run(['kubectl','--kubeconfig',str(LAB/'admin-kubeconfig'),'--context','kind-mini-fury-lab','get','secret',secret_name,'-n',namespace,'-o','name'],capture_output=True).returncode==0
            if not found:kube('apply','-f','-',input=json.dumps({'apiVersion':'v1','kind':'Secret','metadata':{'name':secret_name,'namespace':namespace},'type':'Opaque','stringData':{key:value}}),text=True,stdout=subprocess.DEVNULL)
        config['serviceProfiles']=profiles
    private(directory/'connections.json',json.dumps(config,indent=2))
    # This demo secret is generated and submitted through stdin. It is never read by the runtime service account.
    secret={'apiVersion':'v1','kind':'Secret','metadata':{'name':'lab-message','namespace':namespace},'type':'Opaque','stringData':{'message':secrets.token_urlsafe(24)}}
    exists=subprocess.run(['kubectl','--kubeconfig',str(LAB/'admin-kubeconfig'),'--context','kind-mini-fury-lab','get','secret','lab-message','-n',namespace,'-o','name'],cwd=ROOT,capture_output=True).returncode==0
    if not exists:kube('apply','-f','-',input=json.dumps(secret),text=True,stdout=subprocess.DEVNULL)
    private(directory/'lab.json',json.dumps({'applicationId':args.application_id,'namespace':namespace,'registry':'127.0.0.1:15050','imageRepository':'mini-fury-lab-registry:5000/mini-fury/lab-app/api','target':args.target},indent=2))
    print('Laboratorio listo:',namespace,'/ credenciales privadas generadas; duración del token: 24 h. No se cambió el contexto global.')
if __name__=='__main__':main()
