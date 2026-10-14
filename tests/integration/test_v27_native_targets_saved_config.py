"""Scoped saved locators only; this does not prove graph admission or model quality."""
from copy import deepcopy
from uuid import uuid4

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_v27_target_checkpoint import seed_checkpoint, ROLES


def lookup(db, args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (args[0],))
        row = db.execute('SELECT * FROM photo.semantic_native_targets_transfer_saved_config(%s,%s,%s,%s,%s,%s,%s)', args).fetchone()
        db.execute('RESET ROLE')
        return row


def test_native_targets_saved_config_scope_and_revoke(db):
    packet, case, _, _ = seed_checkpoint(db)
    args = [case['Tenant'],case['Project'],case['Actor'],case['Session'],packet['run_id'],packet['request_sha'],case['Snapshot']]
    config = dict(project_id=args[1],input_snapshot_id=args[6],v27_semantic=dict(auth_session_id=args[3]),v27_native_targets_transfer=dict(schema_version=1,parent_run_id=args[4],request_sha=args[5]))
    template_run=args[4]
    def insert(candidate):
        scoped=args.copy();scoped[4]=str(uuid4())
        candidate=deepcopy(candidate)
        marker=candidate.get('v27_native_targets_transfer',{})
        if marker.get('parent_run_id')==template_run:
            marker['parent_run_id']=scoped[4]
        db.execute("""INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy)
          SELECT %s,tenant_id,kind,input_snapshot_id,%s,config_hash,created_by,status,graph_sealed,failure_policy FROM photo.runs WHERE id=%s""",(scoped[4],Jsonb(candidate),template_run))
        return scoped,candidate
    args,config=insert(config)
    assert lookup(db,args)[0] == config
    assert lookup(db,args)[0] == config
    for i in range(7):
        changed=args.copy();changed[i]='f'*64 if i==5 else str(uuid4())
        assert lookup(db,changed) is None
    for path in ['marker','schema','parent','request','session','project','snapshot']:
        changed=deepcopy(config)
        if path=='marker': changed.pop('v27_native_targets_transfer')
        elif path=='schema': changed['v27_native_targets_transfer']['schema_version']=2
        elif path=='parent': changed['v27_native_targets_transfer']['parent_run_id']=str(uuid4())
        elif path=='request': changed['v27_native_targets_transfer']['request_sha']='e'*64
        elif path=='session': changed['v27_semantic']['auth_session_id']=str(uuid4())
        elif path=='project': changed['project_id']=str(uuid4())
        else: changed['input_snapshot_id']=str(uuid4())
        if changed.get('v27_native_targets_transfer',{}).get('parent_run_id')==args[4]:
            changed['v27_native_targets_transfer']['parent_run_id']=template_run
        scoped,_=insert(changed)
        assert lookup(db,scoped) is None
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
    assert lookup(db,args) is None
    db.execute('UPDATE photo.sessions SET revoked_at=NULL WHERE id=%s',(args[3],))
    assert lookup(db,args)[0] == config


def test_native_targets_saved_config_denied_roles(db):
    signature='photo.semantic_native_targets_transfer_saved_config(uuid,uuid,uuid,uuid,uuid,text,uuid)'
    for role in ROLES:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] == (role=='photo_api')
        if role != 'photo_api':
            with pytest.raises(errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute('SET LOCAL ROLE '+role)
                    db.execute('SELECT * FROM photo.semantic_native_targets_transfer_saved_config(%s,%s,%s,%s,%s,%s,%s)',[str(uuid4()) for _ in range(5)]+['f'*64,str(uuid4())])
