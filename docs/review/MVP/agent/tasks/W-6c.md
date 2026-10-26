---
id: W-6c
title: provision.py - broker certificate and amqps URL
area: deploy
size: S
depends: [W-6b]
edit:
  - deploy/framevela/provision.py
context:
  - deploy/framevela/provision.py:30-56
verify:
  - python3 -m py_compile deploy/framevela/provision.py
  - python3 -c "t=open('deploy/framevela/provision.py').read(); assert 'amqps://' in t and 'rabbitmq' in t and 'amqp://framevela' not in t"
---
## Goal
The installer must issue a server certificate for RabbitMQ from the existing internal CA and write an `amqps://` URL.

## Spec (the file uses compact 1-space-indented code; keep the style)
1. In the tuple list of the `for name,cn,ext,uid in [...]` loop add a new entry for the broker: `('rabbit','rabbitmq','subjectAltName=DNS:rabbitmq,DNS:queue.photo.internal\nextendedKeyUsage=serverAuth', <uid of the rabbitmq container user>)` - use `999` for the uid. The loop writes certificates into `folder = sec/'postgres' if name=='postgres' else gw`; extend that expression so `name=='rabbit'` uses `sec/'rabbitmq'` and also copies/links the CA there: after the loop add code that, if `(sec/'rabbitmq'/'ca.crt')` does not exist, copies `gw/'ca.crt'` to it (`shutil.copyfile`, add the import if missing) with mode 0o644 and `os.chown(..., 999, 999)`.
2. The compose mount names are `rabbitmq/server.crt` and `rabbitmq/server.key`; so for this entry the file stem must be `server`: pass `name='server'` handling by adding a separate small helper or by renaming only for the rabbit case (choose the minimal change; the existing loop uses `name` both for file names and the postgres branch).
3. Replace `f"amqp://framevela:{v['rabbit']}@rabbitmq:5672/photo"` by `f"amqps://framevela:{v['rabbit']}@rabbitmq:5671/photo"`.
4. Nothing else.
