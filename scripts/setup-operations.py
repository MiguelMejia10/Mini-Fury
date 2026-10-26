#!/usr/bin/env python3
"""Generate local-only service secrets without replacing existing configuration."""
import base64
import os
from pathlib import Path
import secrets
ROOT=Path(__file__).resolve().parents[1]
def create(path,content):
    path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    try:
        fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
    except FileExistsError:
        print('Conservado:',path.relative_to(ROOT));return
    with os.fdopen(fd,'w') as f:f.write(content)
    print('Creado:',path.relative_to(ROOT))
def main():
    local=ROOT/'.local'
    create(local/'metrics_token',secrets.token_urlsafe(32))
    create(local/'grafana_password',secrets.token_urlsafe(32))
    token=(local/'metrics_token').read_text().strip()
    create(local/'runtime.env','\n'.join([
        '# SOLO desarrollo local. Para producción configura tus proveedores y HTTPS.',
        'DELIVERY_KEY='+base64.b64encode(secrets.token_bytes(32)).decode(),
        'METRICS_TOKEN='+token,
        'SMTP_ADDRESS=mailpit:1025','SMTP_FROM=no-reply@mini-fury.local',
        'SMTP_ALLOW_LOCAL=true','APP_PUBLIC_URL=http://127.0.0.1:5173',
        '# ALERT_WEBHOOK_URL y ALERT_WEBHOOK_SECRET se configuran solo al activar un destino.',
        '']) )
    create(local/'backup.env','RESTIC_REPOSITORY=/repository\nRESTIC_PASSWORD='+secrets.token_urlsafe(48)+'\nBACKUP_INTERVAL_SECONDS=86400\n')
if __name__=='__main__':main()
