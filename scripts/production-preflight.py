#!/usr/bin/env python3
"""Validate production Compose configuration without printing resolved secrets."""
import argparse
import base64
import json
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit
ROOT=Path(__file__).resolve().parents[1]
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--env-file',default='.local/production.env');parser.add_argument('--allow-local-test',action='store_true');args=parser.parse_args()
    result=subprocess.run(['docker','compose','--env-file',args.env_file,'-f','compose.production.yml','config','--format','json'],cwd=ROOT,capture_output=True,text=True)
    if result.returncode:raise ValueError('Falta configuración de producción. Revisa los archivos de ejemplo; no se mostraron secretos.')
    config=json.loads(result.stdout);services=config['services'];env=services['platform-api']['environment'];origin=urlsplit(env.get('APP_PUBLIC_URL',''))
    if origin.scheme!='https' or not origin.hostname or origin.path or origin.query or origin.fragment:raise ValueError('APP_PUBLIC_URL debe ser un origen HTTPS sin ruta.')
    site=services['web']['environment'].get('SITE_ADDRESS','')
    if site.startswith('http://'):raise ValueError('SITE_ADDRESS no puede deshabilitar HTTPS.')
    if not args.allow_local_test and (origin.hostname in ['localhost','127.0.0.1'] or origin.hostname.endswith('.example.com')):raise ValueError('Configura un dominio real antes de publicar.')
    if env.get('COOKIE_SECURE')!='true' or not env.get('TRUSTED_PROXY_CIDRS'):raise ValueError('Revisa cookies y proxy de confianza.')
    if env.get('ALLOWED_ORIGINS')!=env.get('APP_PUBLIC_URL'):raise ValueError('El origen público y el origen permitido deben coincidir.')
    if env.get('SMTP_ALLOW_LOCAL')=='true' or env.get('WEBHOOK_ALLOW_LOCAL')=='true':raise ValueError('Los destinos sin TLS solo se permiten en desarrollo.')
    if env.get('SMTP_ADDRESS') or env.get('ALERT_WEBHOOK_URL'):
        if len(base64.b64decode(env.get('DELIVERY_KEY',''),validate=True))!=32:raise ValueError('Configura una clave independiente para la cola cifrada.')
    for name in ['postgres','platform-api']:
        if services[name].get('ports'):raise ValueError('API y PostgreSQL deben permanecer sin puertos públicos.')
    password=services['postgres']['environment'].get('POSTGRES_PASSWORD','')
    if len(password)<24 or 'replace_' in password or 'change_me' in password:raise ValueError('Usa una contraseña de base de datos única de al menos 24 caracteres.')
    print('Configuración válida: HTTPS, cookies, origen, proxy y servicios privados. DNS, certificados públicos y proveedores requieren activación externa.')
if __name__=='__main__':
    try:main()
    except (ValueError,OSError,KeyError) as error:print(str(error),file=sys.stderr);sys.exit(1)
