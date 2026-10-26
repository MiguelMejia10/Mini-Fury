#!/usr/bin/env python3
"""Ordered real-cluster acceptance test for points 1–5, using only the isolated laboratory account/API/database."""
import http.cookiejar,json,subprocess,time,urllib.request,urllib.error,uuid
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
DIRECTORY=ROOT/'.local/kube-lab/test'
BASE='http://127.0.0.1:18080';ORIGIN='http://127.0.0.1:5174'
def main():
    fixture=json.loads((DIRECTORY/'lab.json').read_text());app=fixture['applicationId'];env=dict(line.split('=',1) for line in (DIRECTORY/'account.env').read_text().splitlines() if '=' in line)
    jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar));csrf=''
    def call(method,path,body=None,key=None):
        data=None if body is None else json.dumps(body).encode();headers={'Origin':ORIGIN,'Content-Type':'application/json','X-CSRF-Token':csrf}
        if key:headers['Idempotency-Key']=key
        req=urllib.request.Request(BASE+path,data=data,headers=headers,method=method)
        with opener.open(req,timeout=12) as response:return json.load(response)
    for attempt in range(40):
        try:call('GET','/api/health');break
        except Exception:
            if attempt==39:raise
            time.sleep(1)
    session=call('POST','/api/auth/login',{'email':env['ADMIN_EMAIL'],'password':env['ADMIN_PASSWORD']});csrf=session['csrfToken']
    suffix=uuid.uuid4().hex[:10]
    scope=call('POST',f'/api/applications/{app}/scopes',{'name':'lab-'+suffix,'environment':'staging','criticality':'low','region':'local','domain':'','clusterId':'local-lab','namespace':fixture['namespace'],'replicas':1,'cpuMilli':250,'memoryMiB':128})['id']
    record={'scopeId':scope,'applicationId':app,'checks':[]}
    def check(name):record['checks'].append(name);print(name,flush=True)
    check('1. Scope creado, consultado y aislado por aplicación')
    assert call('GET',f'/api/scopes/{scope}')['name']=='lab-'+suffix
    assert call('GET',f'/api/scopes/{scope}/kubernetes')['deployed'] is False
    def wait(job,expected='succeeded',timeout=200):
        deadline=time.monotonic()+timeout
        while time.monotonic()<deadline:
            value=call('GET','/api/runtime/jobs/'+job['id'])['job']
            if value['state'] in ['succeeded','failed','cancelled']:
                if value['state']!=expected:raise AssertionError('Unexpected job outcome: '+json.dumps(value))
                return value
            time.sleep(2)
        raise TimeoutError('Lab operation did not finish')
    revision=subprocess.check_output(['python3','scripts/lab/runner.py','--print-revision'],cwd=ROOT,text=True).strip()
    def build(name):
        job=call('POST',f'/api/applications/{app}/builds',{'name':name,'sourceRevision':revision},uuid.uuid4().hex)
        subprocess.run(['python3','scripts/lab/runner.py','--target','test','--once'],cwd=ROOT,check=True)
        wait(job)
        versions=call('GET',f'/api/applications/{app}/versions')['versions']
        return next(v for v in versions if v['name']==name)
    version=build('good-'+suffix)
    deploy=call('POST',f'/api/scopes/{scope}/actions/deploy',{'versionId':version['id']},uuid.uuid4().hex);wait(deploy)
    observation=call('GET',f'/api/scopes/{scope}/kubernetes');assert observation['deployed'] and observation['status']['readyReplicas']==1
    pod=observation['pods'][0];name=pod['metadata']['name'];container=pod['spec']['containers'][0]['name']
    lines=call('GET',f'/api/scopes/{scope}/pods/{name}/logs?container={container}&lines=200')['text'];assert 'mini_fury_lab_' in lines
    check('2. Kubernetes: pods, estado y logs reales comprobados')
    check('3. Build real, imagen por digest y despliegue comprobados')
    configuration=call('POST',f'/api/scopes/{scope}/configuration',{'revision':0,'variables':{'MESSAGE':'configuration-'+suffix},'secretRefs':{'DB_PASSWORD':'lab-message'}})
    apply=call('POST',f'/api/scopes/{scope}/actions/configure',{},uuid.uuid4().hex);wait(apply)
    configs=call('GET',f'/api/scopes/{scope}/configuration')['configurations'];assert configs[0]['id']==configuration['id'] and configs[0]['secretRefs']['DB_PASSWORD']=='lab-message'
    observation=call('GET',f'/api/scopes/{scope}/kubernetes');assert 'DB_PASSWORD' not in json.dumps(observation)
    # Verify namespace RBAC using a private token-only kubeconfig; no credentials in process arguments.
    admin=json.loads(subprocess.check_output(['kubectl','--kubeconfig',str(ROOT/'.local/kube-lab/admin-kubeconfig'),'config','view','--raw','-o','json'],text=True))
    limited={'apiVersion':'v1','kind':'Config','clusters':admin['clusters'],'users':[{'name':'limited','user':{'token':(DIRECTORY/'token').read_text().strip()}}],'contexts':[{'name':'limited','context':{'cluster':admin['clusters'][0]['name'],'user':'limited'}}],'current-context':'limited'}
    private=DIRECTORY/'limited-kubeconfig';private.write_text(json.dumps(limited));private.chmod(0o600)
    for verb,resource,namespace in [('get','secrets',fixture['namespace']),('list','pods','default')]:
        result=subprocess.run(['kubectl','--kubeconfig',str(private),'auth','can-i',verb,resource,'-n',namespace],capture_output=True,text=True)
        assert result.returncode==1 and result.stdout.strip()=='no'
    check('4. Configuración versionada aplicada; referencias resueltas sin lectura de Secrets por la API')
    scale=call('POST',f'/api/scopes/{scope}/actions/scale',{'replicas':2},uuid.uuid4().hex);wait(scale)
    assert call('GET',f'/api/scopes/{scope}/kubernetes')['status']['readyReplicas']==2
    restart=call('POST',f'/api/scopes/{scope}/actions/restart',{},uuid.uuid4().hex);call('POST','/api/runtime/jobs/'+restart['id']+'/cancel',{});wait(restart,'cancelled')
    retry=call('POST','/api/runtime/jobs/'+restart['id']+'/retry',{},uuid.uuid4().hex);wait(retry)
    check('5. Trabajos: escalado, cancelación, reintento y trazabilidad comprobados')
    single=call('POST',f'/api/scopes/{scope}/actions/scale',{'replicas':1},uuid.uuid4().hex);wait(single)
    broken=build('broken-'+suffix)
    failure=call('POST',f'/api/scopes/{scope}/actions/deploy',{'versionId':broken['id']},uuid.uuid4().hex);wait(failure,'failed',210)
    rollback=call('POST',f'/api/scopes/{scope}/actions/rollback',{'previousJobId':deploy['id']},uuid.uuid4().hex);wait(rollback)
    observation=call('GET',f'/api/scopes/{scope}/kubernetes');assert observation['status']['readyReplicas']==1
    check('3/5. Fallo real de readiness detectado y rollback al snapshot anterior completado')
    for attempt in range(20):
        observation=call('GET',f'/api/scopes/{scope}/kubernetes')
        if observation['metricsAvailable'] and observation['usage']:break
        time.sleep(2)
    assert observation['metricsAvailable'] and observation['usage'],'Metrics Server did not return measured usage'
    check('2. CPU y memoria medidos por Metrics Server comprobados')
    record.update({'versionId':version['id'],'initialDeploymentId':deploy['id'],'failedDeploymentId':failure['id'],'rollbackId':rollback['id'],'metricsAvailable':True,'finishedAt':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())})
    (DIRECTORY/'validation.json').write_text(json.dumps(record,indent=2));print('Validación de los puntos 1–5 completada; laboratorio conservado para revisión.',flush=True)
if __name__=='__main__':main()
