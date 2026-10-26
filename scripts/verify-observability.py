#!/usr/bin/env python3
"""Verify the local telemetry pipeline and optionally persistency across restarts."""
import argparse
import base64
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.parse
import urllib.request
ROOT=Path(__file__).resolve().parents[1]
COMPOSE=['docker','compose','-f','docker-compose.yml','-f','compose.observability.yml']
def internal(url):
    result=subprocess.run(COMPOSE+['exec','-T','prometheus','wget','-qO-',url],cwd=ROOT,capture_output=True,text=True)
    if result.returncode:raise RuntimeError('Servicio histórico aún no disponible.')
    return result.stdout
def read_data(until):
    query=urllib.parse.urlencode({'query':'{app="mini-fury"}','start':str(int((until-600)*1e9)),'end':str(int(until*1e9)),'limit':'10'})
    logs=json.loads(internal('http://loki:3100/loki/api/v1/query_range?'+query))
    count=sum(len(s['values']) for s in logs['data']['result'])
    query=urllib.parse.urlencode({'query':'mini_fury_http_requests_total','start':str(until-600),'end':str(until),'step':'15'})
    metrics=json.loads(internal('http://localhost:9090/api/v1/query_range?'+query))
    points=sum(len(s['values']) for s in metrics['data']['result'])
    if not count or not points:raise RuntimeError('Aún no hay muestras completas. Espera un minuto tras instalar.')
    return count,points
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--restart',action='store_true');args=parser.parse_args()
    for _ in range(3):
        with urllib.request.urlopen('http://127.0.0.1:8080/api/health',timeout=5) as response:assert response.status==200
    targets=json.loads(internal('http://localhost:9090/api/v1/targets'))
    assert all(t['health']=='up' for t in targets['data']['activeTargets'])
    # Authenticate only to the independently generated local Grafana service.
    password=(ROOT/'.local/grafana_password').read_text().strip()
    request=urllib.request.Request('http://127.0.0.1:'+os.environ.get('GRAFANA_PORT','3300')+'/api/dashboards/uid/mini-fury-operations',headers={'Authorization':'Basic '+base64.b64encode(('admin:'+password).encode()).decode()})
    with urllib.request.urlopen(request,timeout=10) as response:dashboard=json.load(response)
    assert len(dashboard['dashboard']['panels'])==4
    until=time.time()-10
    before=read_data(until)
    if args.restart:
        subprocess.run(COMPOSE+['restart','loki','prometheus'],cwd=ROOT,check=True,stdout=subprocess.DEVNULL)
        for attempt in range(50):
            try:
                assert internal('http://loki:3100/ready').strip()=='ready'
                after=read_data(until)
                assert after[0]>=before[0] and after[1]>=before[1]
                break
            except (RuntimeError,AssertionError):
                if attempt==49:raise
                time.sleep(1)
    print(json.dumps({'grafanaPanels':4,'scrapeAuthenticated':True,'historicalLogLines':before[0],'historicalMetricPoints':before[1],'restartPersistenceVerified':args.restart}))
if __name__=='__main__':main()
