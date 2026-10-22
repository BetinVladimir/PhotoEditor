#!/usr/bin/env python3
import pathlib,subprocess,os,json
r=pathlib.Path('/srv/photoeditor');s=r/'secrets';m=s/'minio';g=s/'gateway'
def openssl(*args):subprocess.run(['openssl',*map(str,args)],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
if not (m/'public.crt').exists():
 openssl('req','-new','-newkey','rsa:3072','-nodes','-subj','/CN=minio','-keyout',m/'private.key','-out',m/'request.csr')
 (m/'extensions').write_text('subjectAltName=DNS:minio\nextendedKeyUsage=serverAuth\n')
 openssl('x509','-req','-in',m/'request.csr','-CA',g/'ca.crt','-CAkey',g/'ca.key','-CAcreateserial','-days','825','-extfile',m/'extensions','-out',m/'public.crt')
for name in ['public.crt','private.key']:
 os.chown(m/name,10001,10001);(m/name).chmod(0o600 if name=='private.key' else 0o644)
(m/'ca.crt').write_bytes((g/'ca.crt').read_bytes());(m/'ca.crt').chmod(0o644)
image=json.loads((r/'release/image-pins.json').read_text())['PHOTO_API_IMAGE']
bundle=subprocess.check_output(['docker','run','--rm','--entrypoint','cat',image,'/etc/ssl/certs/ca-certificates.crt'])
(s/'trust-bundle.pem').write_bytes(bundle+b'\n'+(g/'ca.crt').read_bytes());(s/'trust-bundle.pem').chmod(0o644)
print('Internal storage TLS and combined public/private trust installed.')
