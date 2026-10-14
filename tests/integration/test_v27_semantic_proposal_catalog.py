"""Catalog authorization only; synthetic metadata is not byte qualification."""
import json
import psycopg
import pytest
from uuid import uuid4, UUID
from psycopg.types.json import Jsonb
from psycopg.pq import TransactionStatus
from layer_original_fixture import seed_original_composition


def test_proposal_catalog_user_mask_scope(db):
    case = seed_original_composition(db, snapshot_canvas=True)
    body = json.loads(case['Params'])
    base = body['base']
    mask = body['mask']['mask']
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,'test',%s,%s,100,'application/json','v1',clock_timestamp(),'document')", (mask['artifact_id'], case['Tenant'], str(uuid4()), mask['sha256']))
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,user_mask_binding) VALUES(%s,%s,%s,%s,%s,%s,100,'test','ready','v1',clock_timestamp(),%s)", (case['Tenant'], uuid4(), case['Project'], base['asset_version_id'], mask['artifact_id'], mask['sha256'], Jsonb(dict(source=base['source'],canvas_hash=base['canvas_hash'],mask={}))))
    args = [case['Tenant'],case['Actor'],case['Session'],base['asset_version_id'],base['source']['artifact_id'],base['source']['sha256'],base['canvas_hash'],mask['artifact_id'],mask['sha256'],case['Snapshot']]
    signature = 'photo.semantic_proposal_catalog(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid)'
    for role, allowed in [('photo_api',True),('public',False),('photo_worker',False),('photo_scheduler',False)]:
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0] is allowed
    def check(values=None, context=None, inventory=False, locked=False):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler' if locked else 'SET LOCAL ROLE photo_api')
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(context or case['Tenant'],))
            try:
                query='SELECT * FROM photo.lock_semantic_proposal_inventory(' if locked else ('SELECT * FROM photo.semantic_proposal_inventory(' if inventory else 'SELECT project_id,item_count FROM photo.semantic_proposal_catalog(')
                return db.execute(query+','.join(['%s']*10)+')', values or args).fetchall()
            finally:
                if db.info.transaction_status != TransactionStatus.INERROR:
                    db.execute('RESET ROLE')
    assert check() == [(UUID(case['Project']),1)]
    inventory=check(inventory=True)
    assert inventory==[(UUID(case['Project']),1,0,UUID(base['asset_version_id']),UUID(base['source']['artifact_id']),base['source']['sha256'],base['canvas_hash'])]
    assert check(locked=True)==inventory
    assert check(context=str(uuid4()),locked=True)==[]
    locked_signature='photo.lock_semantic_proposal_inventory(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid)'
    for role,allowed in [('photo_scheduler',True),('photo_api',False),('public',False),('photo_worker',False)]:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,locked_signature)).fetchone()[0] is allowed
    assert check(context=str(uuid4()),inventory=True)==[]
    malformed=str(uuid4())
    db.execute("INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) SELECT %s,tenant_id,kind,principal_type,principal_id,digest,item_count,clock_timestamp()+interval '1 hour' FROM photo.snapshots WHERE id=%s",(malformed,case['Snapshot']))
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) SELECT tenant_id,%s,ordinal,asset_version_id,artifact_id,sha256,'{}' FROM photo.snapshot_items WHERE snapshot_id=%s",(malformed,case['Snapshot']))
    db.execute("UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s",(malformed,))
    missing=args.copy();missing[9]=malformed
    assert check(missing,inventory=True)==[]
    for index in [0,1,2,3,4,7,9]:
        changed = args.copy();changed[index] = str(uuid4())
        assert check(changed) == [], index
    for index in [5,6,8]:
        changed = args.copy();changed[index] = 'f'*64
        assert check(changed) == [], index
    assert check(context=str(uuid4())) == []
    db.execute("UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s",(case['Project'],))
    assert check() == []
    db.execute("UPDATE photo.projects SET archived_at=NULL WHERE id=%s",(case['Project'],))
    assert check()
    db.execute("UPDATE photo.v2_draft_uploads SET object_version='different' WHERE artifact_id=%s",(mask['artifact_id'],))
    assert check() == []
    db.execute("UPDATE photo.v2_draft_uploads SET object_version='v1' WHERE artifact_id=%s",(mask['artifact_id'],))
    db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",(case['Session'],))
    assert check() == []


@pytest.mark.parametrize('mutation',['session','version','project','mask'])
def test_proposal_inventory_locks_block_revocation(database,mutation):
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            case = seed_original_composition(db, snapshot_canvas=True)
            body = json.loads(case['Params'])
            base = body['base']
            mask = body['mask']['mask']
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,'test',%s,%s,100,'application/json','v1',clock_timestamp(),'document')", (mask['artifact_id'], case['Tenant'], str(uuid4()), mask['sha256']))
            db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,user_mask_binding) VALUES(%s,%s,%s,%s,%s,%s,100,'test','ready','v1',clock_timestamp(),%s)", (case['Tenant'], uuid4(), case['Project'], base['asset_version_id'], mask['artifact_id'], mask['sha256'], Jsonb(dict(source=base['source'],canvas_hash=base['canvas_hash'],mask={}))))
            args = [case['Tenant'],case['Actor'],case['Session'],base['asset_version_id'],base['source']['artifact_id'],base['source']['sha256'],base['canvas_hash'],mask['artifact_id'],mask['sha256'],case['Snapshot']]
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(case['Tenant'],))
        changes={
            'session':("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",case['Session']),
            'version':("UPDATE photo.asset_versions SET review_state='proposed' WHERE id=%s",base['asset_version_id']),
            'project':("UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s",case['Project']),
            'mask':("UPDATE photo.v2_draft_uploads SET state='retired' WHERE artifact_id=%s",mask['artifact_id']),
        }
        query='SELECT * FROM photo.lock_semantic_proposal_inventory('+','.join(['%s']*10)+')'
        with psycopg.connect(**database,autocommit=True) as locked, psycopg.connect(**database,autocommit=True) as writer:
            with locked.transaction(force_rollback=True):
                locked.execute('SET LOCAL ROLE photo_scheduler')
                locked.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
                assert len(locked.execute(query,args).fetchall())==1
                statement,value=changes[mutation]
                with pytest.raises(psycopg.errors.LockNotAvailable):
                    with writer.transaction():
                        writer.execute("SET LOCAL lock_timeout='250ms'")
                        writer.execute(statement,(value,))
                assert len(locked.execute(query,args).fetchall())==1
            assert writer.execute(statement,(value,)).rowcount==1
            with locked.transaction(force_rollback=True):
                locked.execute('SET LOCAL ROLE photo_scheduler')
                locked.execute("SELECT set_config('app.tenant_id',%s,true)",(case['Tenant'],))
                assert locked.execute(query,args).fetchall()==[]
