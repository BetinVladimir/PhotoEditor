"""Prepared charge discriminator scenarios, authored and not executed.

This targeted persisted-receipt fixture seeds the exact catalog result of a
charged complete_v30_interchange_prepared transaction plus storage counter. It
does NOT exercise import authority, parsed occurrence qualification, publication
or object IO. Those require the separate complete import integration fixtures.
No immutable artifact mutation or trigger bypass is used for hostile cases.
"""
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_task_leases import queued_task
from test_artifact_gc_draft_provenance import charged

JSON_CONTRACTS = (
    'EditingRecipeV3', 'StyleV2', 'CorrectionPlan', 'Override', 'LayerStack',
    'MaskRef', 'Params', 'Stats20', 'ResolvedStyle', 'DefectCandidates',
    'DefectReview', 'DustCandidates', 'DefectProposeReport', 'WorkingImage',
    'FaceLandmarks', 'FaceBoxes', 'HistoricalOriginReceipt',
)
ARRAY_CONTRACTS = ('MaskPixels', 'DenseWarpField', 'AbsoluteLUTArray',
                   'DefectLabelMap', 'WorkingPixels', 'WorkingAlpha')
SUPPORTED = tuple((name, 'application/json') for name in JSON_CONTRACTS) + tuple(
    (name, 'application/octet-stream') for name in ARRAY_CONTRACTS) + (
    ('DefectLabelPNG', 'image/png'),
    ('StyleBackgroundAsset', 'image/jpeg'),
    ('StyleBackgroundAsset', 'image/png'),
    ('StyleBackgroundAsset', 'image/tiff'),
)


def prepared_charge(db, contract, media, *, bad_key=False, receipt_media=None):
    case = queued_task(db)
    artifact, token, sha, size = uuid4(), uuid4(), 'e' * 64, 1024
    key = 'tenants/' + str(case['tenant']) + '/runs/' + str(case['run']) + '/documents/' + str(artifact)
    if bad_key:
        key = 'tenants/' + str(uuid4()) + '/runs/' + str(case['run']) + '/documents/' + str(artifact)
    # Values correspond to the publication writer; only the prepared certificate
    # contract/media fields are relevant to this accounting discriminator.
    scope = dict(contract=contract, media_type=receipt_media or media)
    db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at,v30_import_prepared) VALUES(%s,%s,%s,'v30_import_prepared',%s,%s,%s,%s,'test','ready','prepared-v1',clock_timestamp(),%s)",
        (case['tenant'], case['run'], artifact, artifact, uuid4(), sha, size, Jsonb(scope)))
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role,gc_token,gc_lease_until) VALUES(%s,%s,'test',%s,'prepared-v1',%s,%s,%s,'deleting',clock_timestamp(),'v30_import_prepared',%s,clock_timestamp()+interval '60 seconds')",
        (artifact, case['tenant'], key, sha, size, media, token))
    db.execute('UPDATE photo.tenant_usage SET storage_bytes=storage_bytes+%s WHERE tenant_id=%s', (size, case['tenant']))
    return case['tenant'], artifact, size


@pytest.mark.parametrize('contract,media', SUPPORTED)
def test_exact_supported_prepared_charge_contract_and_media(db, contract, media):
    tenant, artifact, size = prepared_charge(db, contract, media)
    before = db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()
    assert charged(db, tenant, artifact) == (size,)
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone() == before
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s', (artifact,)).fetchone() == ('deleting', False)


@pytest.mark.parametrize('contract,media', SUPPORTED)
def test_cross_media_prepared_charge_cannot_become_zero_charge(db, contract, media):
    wrong = 'application/octet-stream' if media != 'application/octet-stream' else 'application/json'
    tenant, artifact, _ = prepared_charge(db, contract, wrong)
    assert_refused_unchanged(db, tenant, artifact)


@pytest.mark.parametrize('contract,media', [('UnknownContract', 'application/json'),
    ('UnknownContract', 'application/octet-stream'), ('StyleBackgroundAsset', 'image/webp'),
    ('DefectLabelPNG', 'image/jpeg')])
def test_unknown_prepared_contract_or_media_refused(db, contract, media):
    tenant, artifact, _ = prepared_charge(db, contract, media)
    assert_refused_unchanged(db, tenant, artifact)


@pytest.mark.parametrize('contract,media', SUPPORTED)
def test_prepared_foreign_canonical_key_refused_without_mutating_artifact(db, contract, media):
    tenant, artifact, _ = prepared_charge(db, contract, media, bad_key=True)
    assert_refused_unchanged(db, tenant, artifact)


@pytest.mark.parametrize('contract,media', [('Params', 'application/json'), ('DefectLabelPNG', 'image/png')])
def test_receipt_media_must_equal_published_artifact_media(db, contract, media):
    tenant, artifact, _ = prepared_charge(db, contract, media, receipt_media='application/octet-stream')
    assert_refused_unchanged(db, tenant, artifact)


def assert_refused_unchanged(db, tenant, artifact):
    before = db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()
    with pytest.raises(errors.CheckViolation, match='ARTIFACT_ACCOUNTING_RECEIPT_MISMATCH'):
        with db.transaction():
            charged(db, tenant, artifact)
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone() == before
    assert db.execute('SELECT state,gc_storage_settled FROM photo.artifacts WHERE id=%s', (artifact,)).fetchone() == ('deleting', False)
