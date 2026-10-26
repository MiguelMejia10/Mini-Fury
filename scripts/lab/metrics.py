#!/usr/bin/env python3
"""Install a verified Metrics Server release only into the named local kind cluster."""
import hashlib,subprocess,urllib.request
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
URL='https://github.com/kubernetes-sigs/metrics-server/releases/download/v0.9.0/components.yaml'
SHA='1cec29a5267809306a2c6ec74a3e449abbb705b4a8beed0c8a1963910f72c79b'
def main():
    with urllib.request.urlopen(URL,timeout=30) as r:data=r.read()
    if hashlib.sha256(data).hexdigest()!=SHA:raise ValueError('metrics manifest checksum mismatch')
    text=data.decode()
    if 'hostPath:' in text or 'privileged: true' in text:raise ValueError('unexpected privileged metrics manifest')
    path=ROOT/'.local/kube-lab/metrics-components.yaml';path.write_bytes(data)
    base=['kubectl','--kubeconfig',str(ROOT/'.local/kube-lab/admin-kubeconfig'),'--context','kind-mini-fury-lab']
    subprocess.run(base+['apply','-f',str(path)],check=True)
    # kind kubelets use self-signed serving certs. This exception applies only to metrics-server in this lab,
    # never to Mini Fury's Kubernetes API client, which always verifies its dedicated CA.
    patch='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
    subprocess.run(base+['patch','deployment','metrics-server','-n','kube-system','--type=json','-p',patch],check=True)
    subprocess.run(base+['rollout','status','deployment/metrics-server','-n','kube-system','--timeout=120s'],check=True)
if __name__=='__main__':main()
