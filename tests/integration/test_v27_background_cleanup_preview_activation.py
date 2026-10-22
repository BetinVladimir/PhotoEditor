"""Atomic activation scenarios with full synthetic input documents.

Real SQL authority and schema-valid synthetic documents. No function doubles,
no physical object-store or photographic qualification claim. Tests roll back.
"""
import hashlib
import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
from photo_worker.v2.background_cleanup_qualification_report import BACKGROUND_CLEANUP_REQUIRED_CHECKS

from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_background_cleanup_preview_saved_authority import saved_authority_fixture, save, inspect
from test_v27_background_cleanup_preview_quota_authority import reserve, attach_share_scope
from background_cleanup_preview_documents_fixture import (
    working_image_builder, protection_builder, transfer_params_builder, candidate_builder,
)
from semantic_qualification_fixture import frozen_qualification


def prepared_params_fixture(db, *, with_alpha=False, review_documents=False):
    if review_documents:
        from background_cleanup_apply_documents_fixture import prepared_review_fixture
        args,parent,scope,target = prepared_review_fixture(db,with_alpha=with_alpha)
    else:
        args, parent, scope, target = saved_authority_fixture(db,
            source_builder=working_image_builder(with_alpha=with_alpha),metadata_bodies=True,
            protection_builder=protection_builder(db),transfer_params_builder=transfer_params_builder,
            transfer_output_builder=candidate_builder(db))
    pin = frozen_qualification(db,args[0],scope['project'],scope['actor'],
        operation='semantic.preview_background_cleanup',backend='cpu',
        required_checks=sorted(BACKGROUND_CLEANUP_REQUIRED_CHECKS))
    import json
    body = json.loads(args[5])
    body.update(protection=scope['protection']['target_protection'],context_pixels=128,feather_pixels=8,
                **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha')})
    typed = SemanticBackgroundCleanupPreviewInput.model_validate_json(canonical_bytes(body))
    raw = canonical_bytes(typed).decode()
    args = (*args[:5],raw,hashlib.sha256(raw.encode()).hexdigest())
    run = save(db,args)
    pixels = 16*16
    estimate = dict(ram_bytes=768*pixels+536870912+1,vram_bytes=0,
        scratch_bytes=32*pixels+4*16384+16777216+5*pixels+1048576+65536,
        output_bytes=12*pixels+16384+20*pixels+4*65536+8*1048576,pixel_count=pixels)
    profile = dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate,
        model_lock_id=pin['model_lock_sha'],qualification=pin,
        **{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    scope.update(preview_params=typed,preview_profile=profile,parent_run=parent,target=target)
    return args,run,scope


@pytest.mark.parametrize('with_alpha',[False,True])
def test_background_cleanup_full_params_source_protection_candidate_reserve(database,with_alpha):
    args,run,scope = prepared_params_fixture(database,with_alpha=with_alpha)
    assert inspect(database,args,run) == [('draft','',False,False)]
    # Supply normal bootstrap ledger without resetting existing counters.
    database.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING',(args[0],))
    assert database.execute('SELECT count(*) FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()[0] == 1
    assert reserve(database,args,run,scope['source_body'].decode(),scope['preview_profile']) is True
    assert reserve(database,args,run,scope['source_body'].decode(),scope['preview_profile']) is True
    assert save(database,args) == run
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0


def publish_document(db,args,run,task,purpose,raw):
    from uuid import uuid5
    ref = dict(artifact_id=str(uuid5(task,'Params' if purpose=='task_params' else purpose)),sha256=hashlib.sha256(raw).hexdigest())
    values = (*args[:5],run,args[5],args[6],purpose,task,ref['artifact_id'],ref['sha256'],len(raw),'fixture')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        outcome,aid,token,ready = db.execute('SELECT * FROM photo.reserve_semantic_background_cleanup_preview_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()
        assert outcome in ('created','resumed','replayed') and str(aid)==ref['artifact_id']
        if not ready:
            assert db.execute('SELECT photo.complete_semantic_background_cleanup_preview_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (*args[:5],run,args[5],args[6],purpose,task,aid,token,ref['sha256'],len(raw),'fixture','metadata-only-v1')).fetchone()[0] is True
        db.execute('RESET ROLE')
    return ref


def published_transport_fixture(db, *, with_alpha=False, review_documents=False):
    import json
    from uuid import UUID,uuid5
    args,run,scope = prepared_params_fixture(db,with_alpha=with_alpha,review_documents=review_documents)
    # Positive transport fixture owns a fresh tenant; normal bootstrap creates
    # its usage row. Preserve any existing counters and all refusal mutations.
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING',(args[0],))
    assert reserve(db,args,run,scope['source_body'].decode(),scope['preview_profile']) is True
    p = scope['preview_params'].model_dump(mode='json')
    task = uuid5(run,'semantic.preview_background_cleanup:'+p['target_id'])
    params = publish_document(db,args,run,task,'task_params',args[5].encode())
    candidate = json.loads(scope['candidate_body'])
    source = json.loads(scope['source_body'])
    mask = json.loads(scope['candidate_mask_body'])
    protection = json.loads(scope['protection_documents']['target_protection']['body'])
    roots = [(params,'Params'),(p['plan'],'SemanticPlan'),(p['intent'],'EditIntent'),
        (p['candidate'],'TransferCandidate'),(p['source']['source'],'WorkingImage'),(p['protection'],'MaskRef'),
        (candidate['mask'],'MaskRef'),(candidate['transform'],'ResolvedTransform'),
        (source['pixels'],'NpyArray'),(mask['artifact'],'NpyArray'),(protection['artifact'],'NpyArray')]
    if source.get('alpha') is not None:
        roots.append((source['alpha'],'NpyArray'))
    entries = [dict(ref=r,contract=c) for r,c in roots]
    entries.sort(key=lambda e:UUID(e['ref']['artifact_id']).int)
    entries_sha = hashlib.sha256(canonical_bytes(entries)).hexdigest()
    digest = hashlib.sha256(canonical_bytes(dict(entries=entries,operation='semantic.preview_background_cleanup',task=str(task),version='1'))).hexdigest()
    chunk = publish_document(db,args,run,task,'v2_input_chunk_0',canonical_bytes(dict(entries=entries,schema_version=1)))
    manifest = publish_document(db,args,run,task,'v2_input_manifest',canonical_bytes(dict(chunks=[chunk],entry_count=len(entries),entries_sha=entries_sha,schema_version=1,task_id=str(task))))
    node = dict(id=str(task),node_key='semantic.preview_background_cleanup:'+p['target_id'],
        operation='semantic.preview_background_cleanup',operation_version='1',resource_class='gpu',
        params_artifact_id=params['artifact_id'],params_sha=args[6],input_digest=digest,execution_profile=scope['preview_profile'])
    transport = dict(task_id=str(task),entry_count=len(entries),entries_sha=entries_sha,manifest=manifest,chunks=[chunk],direct_roots=[e['ref'] for e in entries])
    inventory = dict(task_id=str(task),entries=entries)
    scope.update(preview_node=node,preview_transport=transport,preview_inventory=inventory)
    return args,run,scope


@pytest.mark.parametrize('with_alpha',[False,True])
def test_background_cleanup_real_document_tickets_complete_transport(database,with_alpha):
    from psycopg.types.json import Jsonb
    args,run,scope = published_transport_fixture(database,with_alpha=with_alpha)
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_fn_owner')
        assert database.execute('SELECT photo.semantic_background_cleanup_preview_documents_ready(%s,%s,%s,%s,%s,%s)',
            (args[0],run,Jsonb(scope['preview_params'].model_dump(mode='json')),Jsonb(scope['preview_node']),Jsonb(scope['preview_transport']),Jsonb(scope['preview_inventory']))).fetchone()[0] is True
        database.execute('RESET ROLE')
    assert inspect(database,args,run) == [('draft','',False,False)]


def commit_preview(db,args,run,scope):
    from psycopg.types.json import Jsonb
    docs = (scope['candidate_body'],scope['intent_body'],scope['source_body'],scope['source_body'],
            scope['candidate_mask_body'],scope['candidate_transform_body'],
            scope['protection_documents']['target_protection']['body'])
    values = (*args[:5],run,args[5],args[6],*(raw.decode() for raw in docs),
        Jsonb(scope['preview_node']),Jsonb(scope['preview_transport']),Jsonb(scope['preview_inventory']))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        task = db.execute('SELECT photo.commit_semantic_background_cleanup_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()[0]
        db.execute('RESET ROLE')
        return task


@pytest.mark.parametrize('with_alpha',[False,True])
def test_background_cleanup_atomic_activation_and_recovery_identity(database,with_alpha):
    args,run,scope = published_transport_fixture(database,with_alpha=with_alpha)
    task = commit_preview(database,args,run,scope)
    node,transport = scope['preview_node'],scope['preview_transport']
    assert str(task) == node['id']
    assert inspect(database,args,run) == [('queued','ready',True,True)]
    assert save(database,args) == run
    assert inspect(database,args,run) == [('queued','ready',True,True)]
    row = database.execute('SELECT state,params_artifact_id,input_digest,inputs,execution_profile FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    assert row[0] == 'ready' and str(row[1]) == node['params_artifact_id'] and row[2] == node['input_digest']
    assert row[3] == [transport['manifest'],*transport['direct_roots']]
    assert row[4]['v2_entries_sha'] == transport['entries_sha']
    assert row[4]['v2_entry_count'] == transport['entry_count']
    count = database.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',(task,)).fetchone()[0]
    assert count == len(scope['preview_inventory']['entries'])+2
    registered = database.execute('SELECT ordinal,artifact_id,sha256,contract FROM photo.v2_task_inputs WHERE task_id=%s ORDER BY ordinal',(task,)).fetchall()
    expected = [(i,entry['ref']['artifact_id'],entry['ref']['sha256'],entry['contract']) for i,entry in enumerate(scope['preview_inventory']['entries'])]
    expected += [(1000000,transport['chunks'][0]['artifact_id'],transport['chunks'][0]['sha256'],'InputClosureChunk'),
                 (1000001,transport['manifest']['artifact_id'],transport['manifest']['sha256'],'InputClosureManifest')]
    assert [(ordinal,str(aid),sha,contract) for ordinal,aid,sha,contract in registered] == expected
    reservation = database.execute("SELECT state,bytes,files,share_id FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run)).fetchone()
    assert reservation == ('reserved',scope['preview_profile']['resource_estimate']['output_bytes'],0,None)
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone() == ('proposed',None,None)


@pytest.mark.parametrize('mutation',['expired_quota','revoked_policy','unverified_source','deleted_asset','unverified_pixels','expired_alpha','share_quota','final_revision_overflow'])
def test_background_cleanup_activation_failure_rolls_back_materialization(database,mutation):
    import psycopg
    args,run,scope = published_transport_fixture(database,with_alpha=mutation == 'expired_alpha')
    if mutation == 'expired_quota':
        database.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(args[0],run))
    elif mutation == 'deleted_asset':
        database.execute('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],))
    elif mutation in ('unverified_pixels','expired_alpha'):
        import json
        source = json.loads(scope['source_body'])
        aid = source['alpha' if mutation == 'expired_alpha' else 'pixels']['artifact_id']
        if mutation == 'expired_alpha':
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(aid,))
        else:
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(aid,))
    elif mutation == 'share_quota':
        attach_share_scope(database,args,run)
    elif mutation == 'revoked_policy':
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(scope['preview_profile']['qualification']['policy_id'],))
    elif mutation == 'unverified_source':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['source'],))
    else:
        # Force the final run UPDATE to fail after registration/sealing/task ready.
        database.execute('UPDATE photo.runs SET revision=9007199254740991 WHERE id=%s',(run,))
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    with pytest.raises((psycopg.errors.CheckViolation,psycopg.errors.SerializationFailure,psycopg.errors.InsufficientPrivilege)):
        commit_preview(database,args,run,scope)
    assert database.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone() == ('draft',False)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0
    assert database.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',(scope['preview_node']['id'],)).fetchone()[0] == 0
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone() == usage


@pytest.mark.parametrize('mutation', ['input_digest','entries_sha','entry_order','missing_entry','manifest_sha','direct_roots'])
def test_background_cleanup_activation_rejects_corrupt_transport_atomically(database,mutation):
    import copy
    import psycopg
    args,run,original = published_transport_fixture(database)
    scope = copy.deepcopy(original)
    node,transport,inventory = scope['preview_node'],scope['preview_transport'],scope['preview_inventory']
    def other_sha(value):
        return ('0' if value[0] != '0' else '1')+value[1:]
    if mutation == 'input_digest':
        node['input_digest'] = other_sha(node['input_digest'])
    elif mutation == 'entries_sha':
        transport['entries_sha'] = other_sha(transport['entries_sha'])
    elif mutation == 'entry_order':
        inventory['entries'].reverse()
    elif mutation == 'missing_entry':
        inventory['entries'].pop()
    elif mutation == 'manifest_sha':
        transport['manifest']['sha256'] = other_sha(transport['manifest']['sha256'])
    else:
        transport['direct_roots'].pop()
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone()
    with pytest.raises(psycopg.errors.CheckViolation):
        commit_preview(database,args,run,scope)
    assert database.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone() == ('draft',False)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0
    assert database.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',(original['preview_node']['id'],)).fetchone()[0] == 0
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(args[0],)).fetchone() == usage
    # Rejecting an altered submission must leave the original draft recoverable.
    assert inspect(database,args,run) == [('draft','',False,False)]


@pytest.mark.parametrize('mutation', [None,'producer_fence','canonical_result','root_role','binary_media','binary_fence'])
def test_background_cleanup_protection_provenance_requires_exact_producer(database,mutation):
    from psycopg.types.json import Jsonb
    args,run,scope = prepared_params_fixture(database)
    doc = scope['protection_documents']['target_protection']
    producer = scope['protection_producer']
    if mutation == 'producer_fence':
        database.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(producer,))
    elif mutation == 'canonical_result':
        _assert_protection_metadata_write_refused(database,args,run,scope,
            'UPDATE photo.tasks SET canonical_result_id=NULL WHERE id=%s',producer,
            constraint='tasks_domain_4')
        mutation = None  # The forbidden write did not alter the healthy producer.
    elif mutation == 'root_role':
        database.execute("UPDATE photo.artifacts SET output_role='document' WHERE id=%s",(doc['ref']['artifact_id'],))
    elif mutation == 'binary_media':
        _assert_protection_metadata_write_refused(database,args,run,scope,
            "UPDATE photo.artifacts SET media_type='application/x-npy' WHERE id=%s",
            doc['binary_ref']['artifact_id'],message='immutable field artifacts.media_type')
        mutation = None  # Reader refusal for initial wrong media is tested separately.
    elif mutation == 'binary_fence':
        database.execute('UPDATE photo.artifacts SET produced_fence=produced_fence+1 WHERE id=%s',(doc['binary_ref']['artifact_id'],))
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_fn_owner')
        ready = database.execute('SELECT photo.semantic_background_cleanup_preview_protection_ready(%s,%s,%s,%s,%s)',
            (args[0],scope['project'],Jsonb(scope['preview_params'].model_dump(mode='json')),doc['binary_ref']['artifact_id'],doc['binary_ref']['sha256'])).fetchone()[0]
        database.execute('RESET ROLE')
    assert ready is (mutation is None)
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0


def _protection_metadata_snapshot(db, tenant):
    # Closed relational evidence, not capability/credential-bearing row dumps.
    return tuple(db.execute(query,(tenant,)).fetchall() for query in (
        'SELECT id,state,attempt,fencing_token,canonical_result_id FROM photo.tasks WHERE tenant_id=%s ORDER BY id',
        'SELECT id,task_id,attempt,fencing_token,outputs FROM photo.task_results WHERE tenant_id=%s ORDER BY id',
        'SELECT id,sha256,bytes,media_type,object_version,output_role,created_by_task_id,produced_attempt,produced_fence FROM photo.artifacts WHERE tenant_id=%s ORDER BY id',
        'SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',
    ))


def _assert_protection_metadata_write_refused(db,args,run,scope,statement,identity,*,constraint=None,message=None):
    import psycopg
    from psycopg.types.json import Jsonb
    doc=scope['protection_documents']['target_protection']
    def ready():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_fn_owner')
            value=db.execute('SELECT photo.semantic_background_cleanup_preview_protection_ready(%s,%s,%s,%s,%s)',
                (args[0],scope['project'],Jsonb(scope['preview_params'].model_dump(mode='json')),
                 doc['binary_ref']['artifact_id'],doc['binary_ref']['sha256'])).fetchone()[0]
            db.execute('RESET ROLE')
            return value
    assert ready() is True
    before=_protection_metadata_snapshot(db,args[0])
    with pytest.raises(psycopg.errors.CheckViolation) as rejected:
        with db.transaction():
            db.execute(statement,(identity,))
    if constraint is not None:
        assert rejected.value.diag.constraint_name == constraint
    if message is not None:
        assert rejected.value.diag.message_primary == message
    assert _protection_metadata_snapshot(db,args[0]) == before
    assert ready() is True
    assert db.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0


def _initial_protection_metadata(db,scope,tenant,*,complete,binary_media):
    """Independent initial SQL producer; no physical object/transfer/Apply proof.

    It reuses the exact typed mask producer construction with a local metadata
    publisher. No already-succeeded task or immutable artifact is rewritten.
    """
    from uuid import uuid4
    def publish(digest,task=None,media='application/json',role=None,body=None):
        assert isinstance(body,bytes)
        assert hashlib.sha256(body).hexdigest() == digest
        artifact=uuid4()
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,'metadata-only-v1',clock_timestamp(),'document',%s,1,1)",
            (artifact,tenant,str(artifact),digest,len(body),binary_media if media=='application/octet-stream' else media,task))
        return artifact
    isolated=dict(scope)
    _metadata_protection_builder(db,complete=complete)(isolated,publish,tenant)
    return isolated


@pytest.mark.parametrize('mutation',[None,'canonical_result','binary_media'])
def test_background_cleanup_initial_protection_producer_predicate(database,mutation):
    # The wrapper's sealed transfer graph remains a separate positive control.
    # This isolates the delegated producer predicate; it does not claim a new
    # incomplete producer can pass the upstream completed-transfer admission.
    from psycopg.types.json import Jsonb
    args,run,scope=prepared_params_fixture(database)
    original=scope['protection_documents']['target_protection']
    isolated=_initial_protection_metadata(database,scope,args[0],
        complete=mutation!='canonical_result',
        binary_media='application/x-npy' if mutation=='binary_media' else 'application/octet-stream')
    doc=isolated['protection_documents']['target_protection']
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_fn_owner')
        assert database.execute('SELECT photo.semantic_background_cleanup_preview_protection_ready(%s,%s,%s,%s,%s)',
            (args[0],scope['project'],Jsonb(scope['preview_params'].model_dump(mode='json')),
             original['binary_ref']['artifact_id'],original['binary_ref']['sha256'])).fetchone()[0] is True
        ready=database.execute('SELECT photo.semantic_protection_producer_ready(%s,%s,%s,%s,%s,%s,%s,%s)',
            (args[0],scope['project'],scope['source'],scope['source_sha'],doc['ref']['artifact_id'],
             doc['ref']['sha256'],doc['binary_ref']['artifact_id'],doc['binary_ref']['sha256'])).fetchone()[0]
        database.execute('RESET ROLE')
    assert ready is (mutation is None)
    state,canonical=database.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',
        (isolated['protection_producer'],)).fetchone()
    assert (state,canonical is not None) == (('ready',False) if mutation=='canonical_result' else ('succeeded',True))
    assert database.execute('SELECT media_type FROM photo.artifacts WHERE id=%s',
        (doc['binary_ref']['artifact_id'],)).fetchone()[0] == ('application/x-npy' if mutation=='binary_media' else 'application/octet-stream')
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(run,)).fetchone()[0] == 0


def _metadata_protection_builder(db, *, complete):
    from io import BytesIO
    from hashlib import sha256
    """Build real relational rasterize provenance; uploaded bytes depend on publisher."""
    def build(scope, publish, tenant):
        import numpy as np
        from uuid import uuid4
        from psycopg.types.json import Jsonb
        from photo_worker.contracts.base import ArtifactRef, canonical_bytes
        from photo_worker.contracts.images import MaskRef, WorkingImage
        image = WorkingImage.model_validate_json(scope['source_body'])
        producer = uuid4()
        mask_run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'preview','running',%s,%s,%s,%s,true) RETURNING id",
            (tenant,scope['snapshot'],Jsonb(dict(project_id=str(scope['project']))),'b'*64,scope['actor'])).fetchone()[0]
        db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id,state) VALUES(%s,%s,%s,'mask_manual',0,'mask.rasterize',%s,'[]','{}',%s,'cpu',%s,'complete')",
            (tenant,mask_run,producer,scope['source'],'c'*64,'d'*64))
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'mask/00','mask.rasterize','1','cpu','ready','[]',%s,%s,'{}',1,1)",
            (producer,tenant,mask_run,scope['source'],'e'*64))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",
            (tenant,producer,uuid4()))
        roots, outputs = {}, []
        scope['protection_pixel_inputs'] = []
        scope['protection_documents'] = {}
        for name in ('source_protection','target_protection'):
            image = WorkingImage.model_validate_json(scope.get('reference_body',scope['source_body'])
                if name=='source_protection' else scope['source_body'])
            values = np.zeros((image.canvas.height,image.canvas.width),dtype=np.float32)
            values[0,0] = 1
            stream = BytesIO()
            np.save(stream,values,allow_pickle=False)
            raw = stream.getvalue()
            digest = sha256(raw).hexdigest()
            # Protected rasterizer outputs use the legacy binary media contract.
            # The payload remains NPY; candidate mask_pixels below uses x-npy.
            binary = publish(digest,task=producer,media='application/octet-stream',body=raw)
            mask = MaskRef(artifact=ArtifactRef(artifact_id=binary,sha256=digest),canvas=image.canvas,
                transform_chain=image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=True)
            body = canonical_bytes(mask)
            root_sha = sha256(body).hexdigest()
            root = publish(root_sha,task=producer,body=body)
            db.execute("UPDATE photo.artifacts SET output_role='mask_protected' WHERE id=%s",(binary,))
            db.execute("UPDATE photo.artifacts SET output_role='mask_protected_ref' WHERE id=%s",(root,))
            roots[name] = dict(artifact_id=str(root),sha256=root_sha)
            outputs.extend([roots[name],dict(artifact_id=str(binary),sha256=digest)])
            scope['protection_pixel_inputs'].append((binary,digest,'NpyArray'))
            scope['protection_documents'][name] = dict(body=body,binary=raw,ref=roots[name],binary_ref=dict(artifact_id=str(binary),sha256=digest))
        if complete:
            result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",
                (tenant,producer,Jsonb(outputs),'e'*64)).fetchone()[0]
            db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,producer))
        scope['protection_producer'] = producer
        return roots
    return build

