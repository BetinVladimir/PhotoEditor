"""Real review/admission/presentation authority to receipt SQL linkage.

Synthetic metadata; does not execute the physical verifier or approve Apply.
No authority functions are replaced. Database fixture rolls every row back.
"""
from semantic_target_state_fixture import reject_target_with_revision
import hashlib
import json

import psycopg
import pytest
from psycopg.types.json import Jsonb

from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_background_cleanup_preview_presentation_source import authorized_sidecars_fixture


def receipt_fixture(db, *, review_documents=False):
    documents = {}
    def initial_documents(args,scope,refs):
        # Metadata-only SQL fixture, not a physical WorkingImage verifier.
        image_body = json.loads(scope['source_body']) if review_documents else {}
        image_body['pixels'] = refs['pixels']
        image = json.dumps(image_body,sort_keys=True,separators=(',', ':'))
        refs['working_image']['sha256'] = hashlib.sha256(image.encode()).hexdigest()
        evidence = json.dumps(dict(schema_version=1,params=json.loads(args[5]),
            working_image=refs['working_image'],pixels=refs['pixels'],
            algorithm='big_lama_work_srgb_delta_v1',protected_pixels_unchanged=True,
            outside_mask_unchanged=True,changed_pixels=0,global_pass=True,roi=[0,0,16,16]),
            sort_keys=True,separators=(',', ':'))
        documents.update(working_image=image,preview_evidence=evidence)
        return documents
    args,run,scope,refs,presentation = authorized_sidecars_fixture(db,review_documents=review_documents,_initial_documents=initial_documents)
    body = json.loads(presentation)
    digest = hashlib.sha256(presentation.encode()).hexdigest()
    task,result,root = db.execute("SELECT t.id,t.canonical_result_id,(r.outputs->0->>'artifact_id')::uuid FROM photo.tasks t JOIN photo.task_results r ON r.id=t.canonical_result_id WHERE t.run_id=%s",(run,)).fetchone()
    return args,run,scope,body,presentation,documents['preview_evidence'],documents['working_image'],task,result,root,digest


def seal(db,fixture):
    args,run,_,_,presentation,evidence,image,*_ = fixture
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        value = db.execute('SELECT photo.seal_semantic_background_cleanup_preview_receipt(%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (args[0],run,args[2],args[3],args[5],args[6],presentation,evidence,image)).fetchone()[0]
        db.execute('RESET ROLE')
        return value


def test_background_cleanup_real_receipt_seals_once_without_approving_target(database):
    data = receipt_fixture(database)
    args,run,scope,presentation,_,_,_,task,result,root,digest = data
    receipt = seal(database,data)
    assert receipt == dict(schema_version=1,params_sha=args[6],task_id=str(task),result_id=str(result),attempt=1,fence=1,
        presentation=dict(artifact_id=str(root),sha256=digest),working_image=presentation['working_image'],pixels=presentation['pixels'],numerical_evidence=presentation['numerical_evidence'])
    assert seal(database,data) == receipt
    assert database.execute('SELECT v27_background_cleanup_preview_receipt FROM photo.runs WHERE id=%s',(run,)).fetchone()[0] == receipt
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(args[4],)).fetchone() == ('proposed', None, None)


@pytest.mark.parametrize('document_index',[4,5,6])
def test_background_cleanup_receipt_rejects_changed_document_bytes(database,document_index):
    data = list(receipt_fixture(database))
    data[document_index] += ' '
    with pytest.raises((psycopg.errors.InvalidParameterValue,psycopg.errors.SerializationFailure)):
        seal(database,data)
    assert database.execute('SELECT v27_background_cleanup_preview_receipt FROM photo.runs WHERE id=%s',(data[1],)).fetchone()[0] is None


def test_background_cleanup_receipt_denies_revoked_editor(database):
    data = receipt_fixture(database)
    database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(data[0][3],))
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        seal(database,data)
    assert database.execute('SELECT v27_background_cleanup_preview_receipt FROM photo.runs WHERE id=%s',(data[1],)).fetchone()[0] is None


@pytest.mark.parametrize('field',['params_sha','result_id','fence','presentation'])
def test_background_cleanup_receipt_conflict_preserves_existing_value(database,field):
    # Deliberately corrupt stored receipt as owner to exercise conflict guard;
    # this is not a worker attempt rotation or a client write permission.
    from uuid import uuid4
    data = receipt_fixture(database)
    receipt = seal(database,data)
    changed = dict(receipt)
    changed[field] = (2 if field == 'fence' else str(uuid4()) if field == 'result_id'
        else '0'*64 if field == 'params_sha' else dict(receipt['presentation'],artifact_id=str(uuid4())))
    database.execute('UPDATE photo.runs SET v27_background_cleanup_preview_receipt=%s WHERE id=%s',(Jsonb(changed),data[1]))
    with pytest.raises(psycopg.errors.SerializationFailure):
        seal(database,data)
    assert database.execute('SELECT v27_background_cleanup_preview_receipt FROM photo.runs WHERE id=%s',(data[1],)).fetchone()[0] == changed


def lock_selection(db,data,receipt):
    args = data[0]
    params = json.loads(args[5])
    render,presentation = receipt['working_image'],receipt['presentation']
    values = (args[0],args[1],args[2],args[3],int(params['workflow_revision']),args[4],int(params['target_revision']),
        params['plan']['sha256'],params['candidate']['sha256'],render['artifact_id'],render['sha256'],presentation['artifact_id'],presentation['sha256'])
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        rows = db.execute('SELECT * FROM photo.lock_semantic_background_cleanup_preview_selection(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchall()
        db.execute('RESET ROLE')
        return rows


def test_background_cleanup_selection_locks_exact_shown_receipt_without_approval(database):
    data = receipt_fixture(database)
    receipt = seal(database,data)
    assert lock_selection(database,data,receipt) == [(data[1],receipt)]
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(data[0][4],)).fetchone() == ('proposed',None,None)


@pytest.mark.parametrize('mutation',['render','presentation','producer_fence','target_revision','source_unverified','missing_receipt'])
def test_background_cleanup_selection_denies_stale_or_other_shown_result(database,mutation):
    from uuid import uuid4
    data = receipt_fixture(database)
    receipt = seal(database,data)
    if mutation in ('render','presentation'):
        field = 'working_image' if mutation == 'render' else 'presentation'
        receipt = dict(receipt,**{field:dict(receipt[field],artifact_id=str(uuid4()))})
    elif mutation == 'producer_fence':
        database.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',(data[7],))
    elif mutation == 'target_revision':
        rejected_batch = reject_target_with_revision(database, data[0][4])[2]
    elif mutation == 'source_unverified':
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(data[2]['source'],))
    else:
        database.execute('UPDATE photo.runs SET v27_background_cleanup_preview_receipt=NULL WHERE id=%s',(data[1],))
    with pytest.raises((psycopg.errors.SerializationFailure,psycopg.errors.InsufficientPrivilege)):
        lock_selection(database,data,receipt)
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',(data[0][4],)).fetchone() == (('rejected', None, rejected_batch) if mutation == 'target_revision' else ('proposed', None, None))
