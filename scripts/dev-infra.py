#!/usr/bin/env python3
"""Manage only PhotoEditor's labelled, loopback-only development containers."""
from pathlib import Path
import argparse
import json
import secrets
import subprocess

ROOT=Path(__file__).resolve().parents[1]
IMAGES=json.loads((ROOT/'resources/dev-images.lock.json').read_text())
SERVICES={
    'minio':{'name':'photoeditor-dev-minio','env':'minio.env','data':'minio',
              'mount':'/data','ports':['127.0.0.1:59000:9000','127.0.0.1:59001:9001'],
              'defaults':{'MINIO_ROOT_USER':'photo_test'},'password':'MINIO_ROOT_PASSWORD'},
    'postgres':{'name':'photoeditor-dev-postgres','env':'postgres.env','data':'postgres',
                'mount':'/var/lib/postgresql/data','ports':['127.0.0.1:55432:5432'],
                'defaults':{'POSTGRES_USER':'photo_test','POSTGRES_DB':'photoeditor'},'password':'POSTGRES_PASSWORD'},
    'rabbitmq':{'name':'photoeditor-dev-rabbit','env':'rabbitmq.env','data':'rabbitmq',
                'mount':'/var/lib/rabbitmq','ports':['127.0.0.1:55672:5672','127.0.0.1:55673:15672'],
                'defaults':{'RABBITMQ_DEFAULT_USER':'photo_test'},'password':'RABBITMQ_DEFAULT_PASS'},
}
def run(*args):
    return subprocess.run(['docker',*args],cwd=ROOT,check=True)

def inspect(name):
    result=subprocess.run(['docker','inspect',name],capture_output=True,text=True,cwd=ROOT)
    if result.returncode:
        if 'no such' in result.stderr.lower():return None
        raise SystemExit('Docker unavailable; no containers were changed')
    info=json.loads(result.stdout)[0]
    if info.get('Config',{}).get('Labels',{}).get('photoeditor.scope')!='development':
        raise SystemExit(f'Refusing to manage unrelated container {name}')
    return info

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('action',choices=['up','down','status'])
    parser.add_argument('--service',choices=sorted(SERVICES),help='Manage one development service')
    args=parser.parse_args()
    for key,service in SERVICES.items():
        if args.service is not None and key != args.service:continue
        info=inspect(service['name'])
        if info and key=='minio' and args.action=='up':
            expected=str(ROOT/'work'/service['data'])
            current=next((m.get('Source') for m in info.get('Mounts',[]) if m.get('Destination')=='/data'),None)
            if current!=expected or info['Config']['Image']!=IMAGES[key]:
                # Only this labelled development container; bind data is retained.
                run('stop',service['name']);run('rm',service['name']);info=None
        if args.action=='status':
            print(service['name'],info['State']['Status'] if info else 'absent')
        elif args.action=='down':
            if info:run('stop',service['name'])
        elif info:
            run('update','--memory','1g','--memory-swap','1g','--cpus','2',service['name'])
            if not info['State']['Running']:run('start',service['name'])
        else:
            if key not in IMAGES:raise SystemExit('Build MinIO first with scripts/run-platform.sh python3 scripts/build-minio.py')
            work=ROOT/'work';work.mkdir(exist_ok=True)
            envfile=work/service['env']
            if not envfile.exists():
                values={**service['defaults'],service['password']:secrets.token_urlsafe(32)}
                # Create with restrictive permissions before writing any secret.
                import os
                fd=os.open(envfile,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
                with os.fdopen(fd,'w') as stream:stream.write(''.join(f'{k}={v}\n' for k,v in values.items()))
            if envfile.stat().st_mode&0o077:raise SystemExit(f'Unsafe permissions on {envfile.name}')
            data=work/service['data'];data.mkdir(parents=True,exist_ok=True)
            command=['run','-d','--name',service['name'],'--label','photoeditor.scope=development',
                     '--memory','1g','--memory-swap','1g','--cpus','2','--env-file',str(envfile),
                     '--mount',f"type=bind,source={data},target={service['mount']}"]
            for port in service['ports']:command+=['-p',port]
            run(*command,IMAGES[key])

if __name__=='__main__':main()
