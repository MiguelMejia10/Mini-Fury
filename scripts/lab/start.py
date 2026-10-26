#!/usr/bin/env python3
"""Start/reuse the disposable laboratory without changing the main database or account."""
import json,os,secrets,subprocess,uuid,time,urllib.request
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def run(args,**kw):return subprocess.run(args,cwd=ROOT,check=True,**kw)
def main():
 directory=ROOT/'.local/kube-lab/test';directory.mkdir(parents=True,exist_ok=True,mode=0o700)
 identifier=directory/'application-id'
 if not identifier.exists():identifier.write_text(str(uuid.uuid4()));identifier.chmod(0o600)
 app=str(uuid.UUID(identifier.read_text().strip()))
 account=directory/'account.env'
 if not account.exists():
  fd=os.open(account,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'w') as f:f.write('ADMIN_EMAIL=lab@mini-fury.local\nADMIN_NAME=Laboratorio Mini Fury\nADMIN_PASSWORD='+secrets.token_urlsafe(28)+'\n')
 run(['python3','scripts/lab/setup.py','--target','test','--application-id',app])
 compose=['docker','compose','-f','compose.runtime-lab.yml']
 run(compose+['up','-d','--build','platform-api'])
 for attempt in range(60):
  try:
   with urllib.request.urlopen('http://127.0.0.1:18080/api/health',timeout=3) as response:
    if response.status==200:break
  except Exception:
   if attempt==59:raise
   time.sleep(1)
 # The bootstrap command is idempotent when the laboratory account already exists.
 exists=run(compose+['exec','-T','postgres','psql','-U','mini_fury','-d','mini_fury','-Atc',"SELECT count(*) FROM users WHERE email='lab@mini-fury.local'"],capture_output=True,text=True).stdout.strip()
 if exists=='0':run(compose+['--profile','setup','run','--rm','user-bootstrap'])
 seed="INSERT INTO applications(id,name,repository,port,environment,owner,runtime,health_check_path,created_by) SELECT '"+app+"','lab-api','https://example.invalid/mini-fury/lab-app',8080,'staging','Laboratorio','Go','/health',id FROM users WHERE email='lab@mini-fury.local' ON CONFLICT DO NOTHING;"
 run(compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','mini_fury','-d','mini_fury'],input=seed,text=True)
 run(['python3','scripts/lab/metrics.py'])
 print('Laboratorio listo. Inicia Vite con vite.lab.config.ts y el runner local. Cuenta privada: .local/kube-lab/test/account.env')
if __name__=='__main__':main()
