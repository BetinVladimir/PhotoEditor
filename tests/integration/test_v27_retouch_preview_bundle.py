"""Exact immutable retouch output bundle; independent gates remain mandatory."""
from hashlib import sha256
from io import BytesIO

import numpy as np
from PIL import Image
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import RetouchPreviewPresentation,RetouchPreviewNumericalEvidence
from retouch_preview_bundle_fixture import retouch_bundle_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config

ROLES=('preview_presentation','working_image','pixels','preview_evidence','before','after','affected','protected')


def bundle_ready(db,data,*,attempt=1,fence=1):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        value=db.execute('SELECT photo.semantic_retouch_preview_bundle_ready(%s,%s,%s,%s)',
            (data['task'],data['result'],attempt,fence)).fetchone()[0]
        db.execute('RESET ROLE')
        return value


@pytest.mark.parametrize('alpha',[False,True])
def test_own_preview_bundle_has_eight_exact_immutable_outputs(database,alpha):
    data=retouch_bundle_fixture(database,with_alpha=alpha)
    assert bundle_ready(database,data) is True
    assert set(data['bodies'])==set(ROLES)
    for role,raw in data['bodies'].items():
        ref=data['refs'][role]
        assert sha256(raw).hexdigest()==ref.sha256
        row=database.execute('SELECT sha256,bytes,output_role,produced_attempt,produced_fence FROM photo.artifacts WHERE id=%s',
            (ref.artifact_id,)).fetchone()
        assert row==(ref.sha256,len(raw),role,1,1)
        assert database.execute('SELECT completed_at IS NOT NULL FROM photo.output_uploads WHERE artifact_id=%s',
            (ref.artifact_id,)).fetchone()[0] is True
    presentation=RetouchPreviewPresentation.model_validate_json(data['bodies']['preview_presentation'])
    evidence=RetouchPreviewNumericalEvidence.model_validate_json(data['bodies']['preview_evidence'])
    assert canonical_bytes(presentation)==data['bodies']['preview_presentation']
    assert evidence.params==presentation.params==data['scope']['retouch_params']
    assert evidence.working_image==presentation.working_image==data['refs']['working_image']
    assert evidence.pixels==presentation.pixels==data['refs']['pixels']
    values=np.load(BytesIO(data['bodies']['pixels']),allow_pickle=False)
    assert values.dtype==np.float32 and values.shape==(16,16,3) and np.all(values==.5)
    assert (data['image'].alpha is not None) is alpha
    for role in ('before','after','affected','protected'):
        with Image.open(BytesIO(data['bodies'][role])) as image:
            assert image.format=='PNG' and image.size==(16,16)
            image.verify()
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
        (data['run'],)).fetchone()[0] is None
    assert database.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s',
        (data['target'],)).fetchone()==('proposed',None,None)


@pytest.mark.parametrize('mutation',['missing_'+role for role in ROLES]+
    ['stale_'+role for role in ROLES]+['wrong_role_'+role for role in ROLES]+
    ['wrong_media_'+role for role in ROLES]+['extra_output','foreign_operation'])
def test_retouch_bundle_refuses_incomplete_foreign_or_stale_inventory(database,mutation):
    # Build final wrong metadata before INSERT, respecting immutable columns.
    data=retouch_bundle_fixture(database,mutation=mutation)
    assert bundle_ready(database,data) is False


@pytest.mark.parametrize('attempt,fence',[(2,1),(1,2),(0,1),(1,0)])
def test_retouch_bundle_exact_attempt_and_fence(database,attempt,fence):
    data=retouch_bundle_fixture(database)
    assert bundle_ready(database,data) is True
    assert bundle_ready(database,data,attempt=attempt,fence=fence) is False


def test_complete_bundle_alone_cannot_publish_or_seal_a_preview_receipt(database):
    data=retouch_bundle_fixture(database)
    assert bundle_ready(database,data) is True
    args,scope=data['args'],data['scope']
    context=scope['retouch_context_ref']
    # No registered qualified closure/admission is installed by this fixture.
    # A plausible eight-output result must not substitute for those gates.
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        rows=database.execute('SELECT * FROM photo.semantic_retouch_preview_presentation_source('+','.join(['%s']*8)+')',
            (args[0],data['run'],args[2],args[3],args[5],args[6],context.artifact_id,context.sha256)).fetchall()
        database.execute('RESET ROLE')
        assert rows==[]
    with pytest.raises(psycopg.errors.SerializationFailure):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute('SELECT photo.seal_semantic_retouch_preview_receipt('+','.join(['%s']*15)+')',
                (args[0],data['run'],args[2],args[3],args[5],args[6],context.artifact_id,context.sha256,
                 data['task'],data['result'],1,1,data['bodies']['preview_presentation'].decode(),
                 data['bodies']['preview_evidence'].decode(),data['bodies']['working_image'].decode()))
    assert database.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
        (data['run'],)).fetchone()[0] is None



def test_retouch_bundle_rejects_empty_canonical_outputs_with_all_eight_artifacts(database):
    # Isolate the canonical-root predicate from the eight-output cardinality.
    data=retouch_bundle_fixture(database,mutation='empty_result')
    assert database.execute('SELECT outputs FROM photo.task_results WHERE id=%s',
        (data['result'],)).fetchone() == ([],)
    assert database.execute('SELECT count(*) FROM photo.artifacts WHERE tenant_id=%s '
        "AND created_by_task_id=%s AND produced_attempt=1 AND produced_fence=1 AND state='ready'",
        (data['args'][0],data['task'])).fetchone() == (8,)
    assert bundle_ready(database,data) is False


def test_retouch_canonical_result_cannot_reference_absent_presentation(database):
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    from removal_transaction_state import tenant_transaction_state
    data=retouch_bundle_fixture(database)
    assert bundle_ready(database,data) is True
    tenant=data['args'][0]
    missing=uuid4()
    assert database.execute('SELECT count(*) FROM photo.artifacts WHERE tenant_id=%s AND id=%s',
        (tenant,missing)).fetchone() == (0,)
    before=tenant_transaction_state(database,tenant)
    with pytest.raises(psycopg.errors.ForeignKeyViolation) as rejected:
        with database.transaction():
            database.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) "
                "VALUES(%s,%s,1,1,%s,'{}',%s)",
                (tenant,data['task'],Jsonb([dict(artifact_id=str(missing),sha256='a'*64)]),'9'*64))
    assert rejected.value.sqlstate == '23503'
    assert rejected.value.diag.message_primary == 'ARTIFACT_REFERENCE_MISSING'
    assert tenant_transaction_state(database,tenant) == before
    assert bundle_ready(database,data) is True
