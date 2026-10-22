#!/usr/bin/env python3
"""First-install configuration. Run as root on the authorized deployment host.
Does not replace existing credentials, initialize a nonempty external DB volume,
or publish database/broker ports. Generated secrets never go to stdout.
"""
import base64, json, os, pathlib, secrets, subprocess
root=pathlib.Path('/srv/photoeditor'); sec=root/'secrets'; cfg=root/'config'; cfg.mkdir(exist_ok=True)
volume=pathlib.Path('/mnt/HC_Volume_107202006')
if not os.path.ismount(volume): raise SystemExit('Required PostgreSQL volume is not mounted')
for sub in ['auth','mail','gateway','postgres','rabbitmq','minio']:(sec/sub).mkdir(parents=True,exist_ok=True,mode=0o750)
def save(p,data,uid=0,mode=0o600):
 p=pathlib.Path(p)
 if not p.exists():
  with p.open('x') as f:f.write(data)
  p.chmod(mode);os.chown(p,uid,uid)
 return p.read_text() if uid==0 else None
state=sec/'installation.json'
if state.exists(): v=json.loads(state.read_text())
else:
 v={k:secrets.token_hex(24) for k in ['admin','api','auth','scheduler','publisher','maintenance','gateway','worker','mail','rabbit','minio']};v['minio_access']='framevela_'+secrets.token_hex(8)
 save(state,json.dumps(v))
for name,key in [('postgres/password','admin'),('rabbitmq/password','rabbit'),('minio/access','minio_access'),('minio/secret','minio')]:save(sec/name,v[key])
def edkey():
 der=subprocess.check_output(['openssl','genpkey','-algorithm','ED25519','-outform','DER'],stderr=subprocess.DEVNULL)
 pub=subprocess.check_output(['openssl','pkey','-inform','DER','-pubout','-outform','DER'],input=der,stderr=subprocess.DEVNULL)
 return base64.b64encode(der[-32:]).decode(),base64.b64encode(pub[-32:]).decode()
if not (sec/'auth/keys.json').exists():
 seed,pub=edkey(); mail=base64.b64encode(secrets.token_bytes(32)).decode()
 save(sec/'auth/keys.json',json.dumps(dict(active_jwt='jwt-v1',jwt_seed=seed,jwt_public={'jwt-v1':pub},otp_pepper=base64.b64encode(secrets.token_bytes(32)).decode(),active_mail='mail-v1',mail={'mail-v1':mail})),10001)
 save(sec/'mail/keys.json',json.dumps(dict(active_mail='mail-v1',mail={'mail-v1':mail})),10001)
if not (sec/'gateway/signing.json').exists():
 seed,pub=edkey();save(sec/'gateway/signing.json',json.dumps(dict(active='gateway-v1',seed=seed,public={'gateway-v1':pub})),10001)
def openssl(*args):subprocess.run(['openssl',*map(str,args)],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
gw=sec/'gateway'
if not (gw/'ca.crt').exists():
 openssl('req','-x509','-newkey','rsa:3072','-nodes','-days','3650','-subj','/CN=Framevela Internal CA','-keyout',gw/'ca.key','-out',gw/'ca.crt');(gw/'ca.key').chmod(0o600)
for name,cn,ext,uid in [('server','object-gateway','subjectAltName=DNS:object-gateway\nextendedKeyUsage=serverAuth',10001),('worker','framevela-worker','extendedKeyUsage=clientAuth',10001),('postgres','postgres','subjectAltName=DNS:postgres\nextendedKeyUsage=serverAuth',70)]:
 folder=sec/'postgres' if name=='postgres' else gw
 if not (folder/(name+'.crt')).exists():
  openssl('req','-new','-newkey','rsa:3072','-nodes','-subj','/CN='+cn,'-keyout',folder/(name+'.key'),'-out',folder/(name+'.csr'))
  (folder/(name+'.ext')).write_text(ext+'\n')
  openssl('x509','-req','-in',folder/(name+'.csr'),'-CA',gw/'ca.crt','-CAkey',gw/'ca.key','-CAcreateserial','-days','825','-extfile',folder/(name+'.ext'),'-out',folder/(name+'.crt'))
 for suffix in ['.key','.crt']:
  p=folder/(name+suffix);os.chown(p,uid,uid);p.chmod(0o600 if suffix=='.key' else 0o644)
roles={'api':['photo_api','photo_portal','photo_scheduler'],'auth':['photo_auth'],'scheduler':['photo_scheduler'],'publisher':['photo_publisher'],'maintenance':['photo_maintenance'],'gateway':['photo_gateway'],'worker':['photo_worker'],'mail':['photo_mail']}
lines=[]
for k,members in roles.items():
 n='frame_'+k
 lines += [f"CREATE ROLE {n} LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '{v[k]}';",f"GRANT {','.join(members)} TO {n};"]
save(sec/'roles.sql','\n'.join(lines)+'\n')
def dsn(role):return f"postgres://{'photo_bootstrap' if role=='admin' else 'frame_'+role}:{v[role]}@postgres:5432/photoeditor?sslmode=require"
env={'MIGRATION_DATABASE_URL':dsn('admin'),'DATABASE_URL':dsn('api'),'AUTH_DATABASE_URL':dsn('auth'),'AMQP_URL':f"amqp://framevela:{v['rabbit']}@rabbitmq:5672/photo",'S3_ACCESS_KEY':v['minio_access'],'S3_SECRET_KEY':v['minio'],'MINIO_ROOT_USER':v['minio_access'],'MINIO_ROOT_PASSWORD':v['minio'],'RABBITMQ_DEFAULT_PASS':v['rabbit'],'POSTGRES_PASSWORD':v['admin']}
for k in roles:env[k.upper()+'_DATABASE_URL']=dsn(k)
save(root/'.env','\n'.join(k+'='+val for k,val in env.items())+'\n')
print('Persistent secrets, internal certificates and service-role bootstrap prepared; values redacted.')
