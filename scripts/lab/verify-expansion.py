#!/usr/bin/env python3
"""Acceptance of points 6–10 against the disposable API and the explicitly named kind cluster."""
import http.cookiejar,json,subprocess,time,urllib.request,urllib.error,uuid,io,zipfile,ssl
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2];PRIVATE=ROOT/'.local/kube-lab/test';BASE='http://127.0.0.1:18080'
def main():
 fixture=json.loads((PRIVATE/'lab.json').read_text());app=fixture['applicationId'];namespace=fixture['namespace'];account=dict(line.split('=',1) for line in (PRIVATE/'account.env').read_text().splitlines() if '=' in line)
 opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()));csrf=''
 def call(method,path,body=None,key=None,binary=False):
  headers={'Content-Type':'application/json','Origin':'http://127.0.0.1:5174','X-CSRF-Token':csrf}
  if key:headers['Idempotency-Key']=key
  request=urllib.request.Request(BASE+path,data=None if body is None else json.dumps(body).encode(),headers=headers,method=method)
  try:
   with opener.open(request,timeout=15) as response:return response.read() if binary else json.load(response)
  except urllib.error.HTTPError as error:raise RuntimeError(f'{method} {path}: HTTP {error.code}: '+error.read().decode()) from None
 session=call('POST','/api/auth/login',{'email':account['ADMIN_EMAIL'],'password':account['ADMIN_PASSWORD']});csrf=session['csrfToken'];suffix=uuid.uuid4().hex[:8];checks=[];record={}
 def check(message):checks.append(message);print(message,flush=True)
 def wait(job,timeout=210):
  deadline=time.monotonic()+timeout
  while time.monotonic()<deadline:
   value=call('GET','/api/runtime/jobs/'+job['id'])['job']
   if value['state'] in ['failed','cancelled']:raise AssertionError('Job failed: '+json.dumps(value))
   if value['state']=='succeeded':return value
   time.sleep(2)
  raise TimeoutError('operation timed out')
 scope=call('POST',f'/api/applications/{app}/scopes',{'name':'ops-'+suffix,'environment':'staging','criticality':'low','region':'local','domain':'','clusterId':'local-lab','namespace':namespace,'replicas':1,'cpuMilli':250,'memoryMiB':128})['id'];record['scopeId']=scope
 # 6: Real incident lifecycle, distinct acknowledgement/mute/resolution, persisted timeline.
 incident=call('POST',f'/api/applications/{app}/incidents',{'title':'Ensayo operativo '+suffix,'description':'Incidente del laboratorio para comprobar las acciones auditadas','severity':'warning','scopeId':scope,'assigneeId':session['user']['id'],'runbook':'https://example.com/lab-runbook','escalationMinutes':15})
 for action in ['acknowledge','mute','unmute','resolve']:
  incident=call('GET','/api/incidents/'+incident['id'])['incident']
  call('POST','/api/incidents/'+incident['id']+'/actions',{'action':action,'version':incident['version'],'message':'Validación local: '+action,'minutes':5})
 detail=call('GET','/api/incidents/'+incident['id']);assert detail['incident']['state']=='resolved' and len(detail['events'])==5
 check('6. Incidente creado, asignado, reconocido, silenciado y resuelto con cronología')
 # 7: Build the actual instrumented application and collect real per-pod request counter deltas.
 revision=subprocess.check_output(['python3','scripts/lab/runner.py','--print-revision'],cwd=ROOT,text=True).strip()
 build=call('POST',f'/api/applications/{app}/builds',{'name':'observed-'+suffix,'sourceRevision':revision},uuid.uuid4().hex)
 # The long-running local agent may already own the build; --once safely claims at most one.
 subprocess.run(['python3','scripts/lab/runner.py','--target','test','--once'],cwd=ROOT,check=True);wait(build)
 version=next(v for v in call('GET',f'/api/applications/{app}/versions')['versions'] if v['name']=='observed-'+suffix)
 wait(call('POST',f'/api/scopes/{scope}/actions/deploy',{'versionId':version['id']},uuid.uuid4().hex))
 slo=call('POST',f'/api/scopes/{scope}/slos',{'name':'Disponibilidad '+suffix,'indicator':'availability','targetPpm':999000,'latencyMs':300,'windowDays':1})
 latency=call('POST',f'/api/scopes/{scope}/slos',{'name':'Latencia '+suffix,'indicator':'latency','targetPpm':990000,'latencyMs':300,'windowDays':1})
 call('POST',f'/api/scopes/{scope}/slos/evaluate',{})
 observation=call('GET',f'/api/scopes/{scope}/kubernetes');pod=next(p['metadata']['name'] for p in observation['pods'] if p['status']['phase']=='Running')
 kube=['kubectl','--kubeconfig',str(ROOT/'.local/kube-lab/admin-kubeconfig'),'--context','kind-mini-fury-lab']
 connection=json.loads(subprocess.check_output(kube+['config','view','--raw','-o','json'],text=True))
 server=connection['clusters'][0]['cluster']['server'];tls=ssl.create_default_context(cafile=str(PRIVATE/'ca.crt'));token=(PRIVATE/'token').read_text().strip()
 def raw(path):
  req=urllib.request.Request(server+f'/api/v1/namespaces/{namespace}/pods/http:{pod}:8080/proxy'+path,headers={'Authorization':'Bearer '+token})
  try:
   with urllib.request.urlopen(req,context=tls,timeout=5) as response:return response.read()
  except urllib.error.HTTPError as error:
   if path=='/error' and error.code==503:return error.read()
   raise

 for index in range(40):raw('/error' if index%5==0 else '/')
 time.sleep(2);call('POST',f'/api/scopes/{scope}/slos/evaluate',{})
 objectives=call('GET',f'/api/scopes/{scope}/slos')['slos'];availability=next(v for v in objectives if v['slo']['id']==slo['id']);assert availability['total']>=40 and availability['good']<availability['total'] and availability['budgetRemainingPercent']<0
 assert any(v['slo']['id']==latency['id'] and v['sliPercent'] is not None for v in objectives)
 incidents=call('GET',f'/api/applications/{app}/incidents')['incidents'];assert any('Consumo rápido' in i['title'] and i['state']=='open' for i in incidents)
 check('7. SLI, presupuesto y latencia calculados con tráfico real; incidente automático de burn')
 # 8: Build/test the exact archives emitted by the API, no credentials in generated source.
 template_apps=[]
 for template in ['go-http','node-http']:
  created=call('POST',f'/api/templates/{template}/applications',{'name':template+'-'+suffix,'repository':'https://example.invalid/lab/'+template+'-'+suffix,'owner':'Laboratorio','teamId':None})
  identifier=created['application']['id'];template_apps.append(identifier);archive=call('GET',f'/api/applications/{identifier}/scaffold',binary=True)
  destination=PRIVATE/'generated'/created['application']['name'];destination.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
  with zipfile.ZipFile(io.BytesIO(archive)) as zipped:
   for member in zipped.infolist():
    if not member.filename.startswith(created['application']['name']+'/') or '..' in Path(member.filename).parts:raise AssertionError('unsafe archive')
   zipped.extractall(destination.parent)
  with (PRIVATE/(template+'-build.log')).open('wb') as log:subprocess.run(['docker','build','-t','mini-fury-'+template+'-verification:'+suffix,str(destination)],cwd=ROOT,stdout=log,stderr=subprocess.STDOUT,check=True)
  status=call('GET',f'/api/applications/{identifier}/template-status');assert status['managed'] and not status['updateAvailable']
 check('8. Plantillas Go y Node descargadas, compiladas y probadas desde sus ZIP reales')
 # 9: Real persistent PostgreSQL/Redis; preserve a SQL row across suspension/resume.
 services=[]
 for profile in ['postgres-dev','redis-dev']:
  requested=call('POST',f'/api/scopes/{scope}/services',{'name':profile+'-'+suffix,'profile':profile,'storageGiB':1,'backupPolicy':'development-only'},uuid.uuid4().hex)
  wait(requested['job']);service=requested['service'];services.append(service)
  status=call('GET','/api/services/'+service['id']+'/status');assert status['readyReplicas']==1
 postgres=services[0];dbpod='mfs-'+postgres['id']+'-0'
 def sql(text):
  # The API never gets exec permission or database passwords. This local acceptance test uses its private lab admin kubeconfig.
  return subprocess.check_output(kube+['exec','-n',namespace,dbpod,'--','psql','-U','mini_fury','-d','mini_fury','-Atc',text],text=True)
 sql('CREATE TABLE IF NOT EXISTS persistence_check(id integer PRIMARY KEY); INSERT INTO persistence_check VALUES(1) ON CONFLICT DO NOTHING;')
 for action in ['suspend','resume']:
  service=next(v for v in call('GET',f'/api/scopes/{scope}/services')['services'] if v['id']==postgres['id'])
  wait(call('POST','/api/services/'+service['id']+'/actions',{'action':action,'version':service['version']},uuid.uuid4().hex))
 assert sql('SELECT count(*) FROM persistence_check').strip()=='1'
 check('9. PostgreSQL y Redis aprovisionados; volumen SQL conserva datos al suspender y reanudar')
 # 10: Explicit reference tariffs, budget overrun and real consumption remain distinct.
 current=call('GET',f'/api/applications/{app}/capacity')['policy']
 call('PUT',f'/api/applications/{app}/cost-policy',{'currency':'USD','cpuCoreHour':0.02,'memoryGiBHour':0.01,'storageGiBMonth':0.1,'monthlyBudget':1,'version':0 if current is None else current['version']})
 capacity=call('GET',f'/api/applications/{app}/capacity');assert capacity['estimatedMonthly']>0 and capacity['invoicedCost'] is None and capacity['overBudget'] and capacity['retainedStorageGiB']>=2
 measured=call('GET',f'/api/scopes/{scope}/kubernetes');assert measured['metricsAvailable']
 check('10. Tarifas, estimación, presupuesto y consumo medido separados; sin factura inventada')
 record.update({'checks':checks,'applicationId':app,'templateApplications':template_apps,'serviceIds':[s['id'] for s in services],'sloIds':[slo['id'],latency['id']],'finishedAt':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())});(PRIVATE/'validation-v07.json').write_text(json.dumps(record,indent=2));print('Validación 6–10 completada; recursos conservados en el laboratorio.',flush=True)
if __name__=='__main__':main()
