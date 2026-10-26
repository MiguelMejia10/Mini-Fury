#!/bin/sh
# Uses only its named disposable Compose project, never the development database.
set -eu
cd "$(dirname "$0")/.."
mkdir -p .local
umask 077
python3 - <<'PY'
from pathlib import Path
import secrets
Path('.local/https-test.env').write_text('POSTGRES_PASSWORD='+secrets.token_hex(24)+'\nSITE_ADDRESS=localhost\nPUBLIC_ORIGIN=https://localhost:8443\nBIND_IP=127.0.0.1\nHTTP_PORT=8085\nHTTPS_PORT=8443\nRUNTIME_ENV_FILE=.local/https-test-runtime.env\n')
Path('.local/https-test-runtime.env').write_text('# Isolated HTTPS fixture; external integrations disabled.\n')
PY
compose() { docker compose -p mini-fury-https-test --env-file .local/https-test.env -f compose.production.yml "$@"; }
cleanup() { compose down -v >/dev/null; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose up -d --build
ready=0
for n in $(seq 1 40); do
 if compose cp web:/data/caddy/pki/authorities/local/root.crt .local/https-test-ca.crt >/dev/null 2>&1; then ready=1; break; fi
 sleep 1
done
if [ "$ready" -ne 1 ]; then echo 'Local test CA not ready' >&2; exit 1; fi
compose run --rm --entrypoint platform-user -e ADMIN_EMAIL=https-fixture@mini-fury.local -e ADMIN_NAME=HTTPS-fixture -e ADMIN_PASSWORD=Disposable-HTTPS-Only-2026! platform-api
python3 - <<'PY'
import json,ssl,time,urllib.request,urllib.error
context=ssl.create_default_context(cafile='.local/https-test-ca.crt')
base='https://localhost:8443'
for attempt in range(30):
    try:
        with urllib.request.urlopen(base+'/api/health',context=context,timeout=3) as response: assert response.status==200
        break
    except Exception:
        if attempt==29:raise
        time.sleep(1)
with urllib.request.urlopen(base,context=context) as response:
    assert "default-src 'self'" in response.headers['Content-Security-Policy']
    assert 'max-age=31536000' in response.headers['Strict-Transport-Security']
try:
    urllib.request.urlopen(base+'/internal/metrics',context=context)
    raise AssertionError('Internal metrics exposed through public proxy')
except urllib.error.HTTPError as error:assert error.code==404
payload=json.dumps({'email':'https-fixture@mini-fury.local','password':'Disposable-HTTPS-Only-2026!'}).encode()
request=urllib.request.Request(base+'/api/auth/login',data=payload,headers={'Origin':base,'Content-Type':'application/json'})
with urllib.request.urlopen(request,context=context) as response:
    cookie=response.headers['Set-Cookie']
    assert '__Host-mini_fury_session=' in cookie and 'Secure' in cookie and 'HttpOnly' in cookie and 'SameSite=Strict' in cookie
print('HTTPS verified with trusted test CA, security headers, private metrics and Secure session cookie.')
PY
