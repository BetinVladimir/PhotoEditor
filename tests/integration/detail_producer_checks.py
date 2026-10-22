"""Real inserted producer/upload adversarial invariants, work-only/unexecuted."""
from uuid import uuid4
from psycopg.types.json import Jsonb
from psycopg import errors
import pytest

PUBLIC='photo.v23_portrait_detail_event_producer'
PRIVATE='photo.v23_portrait_detail_event_producer_evaluate_core'


def select_pair(db,tenant,project,actor,session,event):
    args=(tenant,project,actor,session,event)
    return tuple(db.execute('SELECT * FROM '+name+'(%s,%s,%s,%s,%s)',args).fetchall()
                 for name in (PUBLIC,PRIVATE))


def check_current_producer(db,tenant,project,actor,session,event,producer):
    """No immutable guard disabled; mutations are rollback-only upload receipts."""
    baseline=select_pair(db,tenant,project,actor,session,event)
    assert len(baseline[0])==len(baseline[1])==1 and baseline[0]==baseline[1]
    assert baseline[0][0][5:9]==(producer['run'],producer['task'],producer['result'],producer['attempt'])
    uploads=db.execute('SELECT u.id,u.worker_id,u.role FROM photo.output_uploads u JOIN photo.tasks t '
        'ON t.tenant_id=u.tenant_id AND t.id=u.task_id WHERE t.tenant_id=%s AND t.run_id=%s ORDER BY u.id',
        (tenant,producer['run'])).fetchall()
    assert len(uploads)==3 and {row[2] for row in uploads}=={'working_image','resolved_style','portrait_detail_report'}
    for index,(upload,worker,role) in enumerate(uploads):
        other_worker=uploads[(index+1)%len(uploads)][1]
        assert other_worker!=worker
        for column,value in [('media_type','image/png'),('worker_id',other_worker),('fence',2),
                ('sha256','9'*64),('bytes',1),('object_version','wrong-version'),('state','pending'),('completed_at',None)]:
            with db.transaction(force_rollback=True):
                # Closed authored column list; never interpolate request input.
                assert db.execute('UPDATE photo.output_uploads SET '+column+'=%s WHERE id=%s AND tenant_id=%s',
                    (value,upload,tenant)).rowcount==1
                assert select_pair(db,tenant,project,actor,session,event)==([],[]),(role,column)
            assert select_pair(db,tenant,project,actor,session,event)==baseline
    # Existing same-project neighbors exercise item/version/asset lineage independently.
    neighbor=db.execute('SELECT i.id,i.asset_id,i.asset_version_id FROM photo.collection_items i '
        'JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id '
        'WHERE i.tenant_id=%s AND a.project_id=%s AND i.asset_version_id<>%s ORDER BY i.id LIMIT 1',
        (tenant,project,producer['asset_version'])).fetchone()
    assert neighbor is not None
    for column,value in zip(('source_item_id','source_asset_id','source_version_id'),neighbor):
        with db.transaction(force_rollback=True):
            assert db.execute('UPDATE photo.apply_nodes SET '+column+'=%s WHERE tenant_id=%s AND id=%s',
                (value,tenant,producer['task'])).rowcount==1
            assert select_pair(db,tenant,project,actor,session,event)==([],[]),column
        assert select_pair(db,tenant,project,actor,session,event)==baseline
    # An actual existing foreign project identity, not a random nonexistent ID.
    foreign_project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Producer scope negative') RETURNING id",(tenant,)).fetchone()[0]
    assert select_pair(db,tenant,foreign_project,actor,session,event)==([],[])
    assert select_pair(db,tenant,project,actor,session,event)==baseline


def upgrade_at_fresh_event(db,tenant,project,actor,session,event,producer,patches,preservation_tenants=()):
    """Use legal Apply graph on old head; never retrobind the earlier correction."""
    # Old head2591 cannot row-lock correction_events; preserve this real prestate.
    with pytest.raises(errors.InsufficientPrivilege,match='correction_events'):
        with db.transaction():
            select_pair(db,tenant,project,actor,session,event)
    tenants=[tenant,*preservation_tenants]
    watched={table:db.execute('SELECT to_jsonb(x) FROM photo.'+table+' x WHERE x.tenant_id=ANY(%s::uuid[]) ORDER BY to_jsonb(x)::text',
        (tenants,)).fetchall() for table in ('runs','tasks','task_results','artifacts','correction_events',
            'task_attempts','output_uploads','v23_portrait_detail_event_links','backend_documents','v2_task_inputs','graph_expansions','quota_reservations')}

    # Old correction remains in its original transaction but is never bound.
    assert db.execute('SELECT count(*) FROM photo.v23_portrait_detail_event_links WHERE tenant_id=%s AND event_id=%s',
        (tenant,event)).fetchone()==(0,)
    assert len(patches)==3
    for patch in patches:  # Forward2592 domain repair then2593/2594 row-lock prerequisites.
        db.execute(patch)
    for table,rows in watched.items():
        assert db.execute('SELECT to_jsonb(x) FROM photo.'+table+' x WHERE x.tenant_id=ANY(%s::uuid[]) ORDER BY to_jsonb(x)::text',
            (tenants,)).fetchall()==rows
    # A fresh correction INSERT establishes its own legal bind transaction.
    fresh=db.execute('INSERT INTO photo.correction_events(tenant_id,project_id,session_id,asset_version_id,'
        'base_document_id,approved_document_id,changed_paths,reason,learn,consent_id,consent_epoch,approved_by,client_event_id) '
        'SELECT tenant_id,project_id,session_id,asset_version_id,base_document_id,approved_document_id,changed_paths,'
        'reason,learn,consent_id,consent_epoch,approved_by,%s FROM photo.correction_events WHERE tenant_id=%s AND id=%s RETURNING id',
        (uuid4(),tenant,event)).fetchone()[0]
    assert fresh!=event
    assert db.execute('SELECT count(*) FROM photo.v23_portrait_detail_event_links WHERE tenant_id=%s AND event_id=%s',
        (tenant,event)).fetchone()==(0,)
    return fresh,event
