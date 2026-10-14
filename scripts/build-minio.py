#!/usr/bin/env python3
"""Build the pinned patched MinIO server; all sources/caches stay in this repo."""
from pathlib import Path
import argparse
import platform
import hashlib
import json
import os
import shutil
import subprocess
ROOT=Path(__file__).resolve().parents[1]
lock=json.loads((ROOT/'resources/minio-source.lock.json').read_text())
def output(*args):return subprocess.check_output(args,cwd=ROOT,text=True).strip()
def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--native',action='store_true',help='Build only for this host; keep Docker image lock unchanged')
    args=parser.parse_args()
    if shutil.disk_usage(ROOT).free<1024**3:raise SystemExit('At least 1GiB free disk required for MinIO build')
    meta=json.loads(output('go','mod','download','-json',lock['module']+'@'+lock['version']))
    if meta.get('Sum')!=lock['sum'] or meta.get('GoModSum')!=lock['go_mod_sum']:
        raise SystemExit('MinIO source lock mismatch')
    source=Path(meta['Dir']).resolve()
    if not source.is_relative_to(ROOT):raise SystemExit('MinIO source cache escaped repository')
    machine=platform.machine() if args.native else output('docker','info','--format','{{.Architecture}}')
    arch={'aarch64':'arm64','arm64':'arm64','x86_64':'amd64','amd64':'amd64'}.get(machine)
    if arch is None:raise SystemExit('Unsupported MinIO build architecture')
    target=ROOT/('.runtime/minio-native' if args.native else '.runtime/minio');target.mkdir(parents=True,exist_ok=True)
    host_os={'Darwin':'darwin','Linux':'linux'}.get(platform.system()) if args.native else 'linux'
    if host_os is None:raise SystemExit('Unsupported native MinIO host')
    env=dict(os.environ,GOOS=host_os,GOARCH=arch,CGO_ENABLED='0',GOWORK='off',GOMAXPROCS='2')
    flags='-s -w -X github.com/minio/minio/cmd.Version=2025-10-15T17:29:55Z -X github.com/minio/minio/cmd.ReleaseTag='+lock['release']+' -X github.com/minio/minio/cmd.CommitID='+lock['commit']+' -X github.com/minio/minio/cmd.ShortCommitID='+lock['commit'][:12]
    subprocess.run(['go','build','-p','2','-mod=readonly','-trimpath','-ldflags',flags,'-o',str(target/'minio-server'),'.'],cwd=source,env=env,check=True)
    shutil.copyfile(source/'LICENSE',target/'LICENSE')
    manifest={**lock,'os':host_os,'architecture':arch,'go_version':output('go','env','GOVERSION'),'binary_sha256':hashlib.sha256((target/'minio-server').read_bytes()).hexdigest()}
    (target/'source-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    if args.native:
        print('Built pinned native MinIO',host_os,arch,manifest['binary_sha256'])
        return
    tag='photoeditor-minio:20251015-'+arch
    subprocess.run(['docker','build','-f',str(ROOT/'deploy/images/minio/Dockerfile'),'-t',tag,str(target)],cwd=ROOT,check=True)
    image=output('docker','image','inspect',tag,'--format','{{.Id}}')
    path=ROOT/'resources/dev-images.lock.json'
    images=json.loads(path.read_text());images['minio']=image;path.write_text(json.dumps(images,indent=2)+'\n')
    print('Built patched MinIO',lock['release'],arch,image)
if __name__=='__main__':main()
