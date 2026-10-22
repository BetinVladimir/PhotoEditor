"""Sidecar metadata boundary only: source authority is a transactional double.

Does not prove authenticated presentation_source, physical PNG/ICC or Apply.
Requires explicit integration database; all mutations roll back.
"""
import hashlib
import json
from uuid import uuid4

import psycopg
import pytest

from test_v27_removal_preview_sql_roles import database, isolated_database_config
from test_v27_background_cleanup_preview_bundle import bundle, replace_body, ROLES


def sidecar_fixture(db, body_mutator=None, invalid_sidecar=None):
    params = dict(kind='background_cleanup')
    raw = None
    def build_presentation(refs):
        nonlocal raw
        presentation = dict(schema_version=1, params=params, width=100, height=80,
                            resize='area_linear_premult_v1', overlay='max_coverage_ceil_alpha_v1',
                            icc_lock_sha='b'*64, srgb_sha='c'*64)
        for role, _ in ROLES:
            field = 'numerical_evidence' if role == 'preview_evidence' else role
            presentation[field] = dict(artifact_id=str(refs[role]), sha256='a'*64)
        if body_mutator is not None:
            body_mutator(presentation)
        raw = json.dumps(presentation, separators=(',', ':'))
        return raw.encode()
    tenant, task, result, refs = bundle(db, invalid_sidecar=invalid_sidecar, presentation_builder=build_presentation)
    root = refs['preview_presentation']
    # Keep the sidecar SQL real; isolate only the upstream authenticated source.
    replace_body(db, 'photo.semantic_background_cleanup_preview_presentation_source(uuid,uuid,uuid,uuid,text,text)',
        f"""BEGIN RETURN QUERY SELECT '{task}'::uuid,'{result}'::uuid,1,1::bigint,
        a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
        FROM photo.artifacts a WHERE a.tenant_id='{tenant}'::uuid AND a.id='{root}'::uuid; END;""")
    args = (tenant,uuid4(),uuid4(),uuid4(),json.dumps(params),'d'*64,raw)
    return args, refs


def read(db, args):
    db.execute('SET LOCAL ROLE photo_api')
    return db.execute('SELECT * FROM photo.semantic_background_cleanup_preview_presentation_sidecars(%s,%s,%s,%s,%s,%s,%s)', args).fetchall()


def test_background_cleanup_sidecars_return_all_exact_role_refs(database):
    args, refs = sidecar_fixture(database)
    rows = read(database,args)
    assert len(rows) == 7
    assert {row[0]: row[1] for row in rows} == {role: refs[role] for role, _ in ROLES}
    assert all(row[2] == 'a'*64 and row[5] == 'v1' for row in rows)


@pytest.mark.parametrize('role', [role for role, _ in ROLES])
@pytest.mark.parametrize('mutation', ['producer','attempt','fence','unverified','version','expired','role','media'])
def test_background_cleanup_sidecars_reject_each_stale_object(database, role, mutation):
    initial = {'version': dict(object_version=None), 'media': dict(media_type='application/octet-stream')}
    args, refs = sidecar_fixture(database, invalid_sidecar=(role,initial[mutation]) if mutation in initial else None)
    changes = {
        'producer': 'created_by_task_id=NULL', 'attempt': 'produced_attempt=2',
        'fence': 'produced_fence=2', 'unverified': 'verified_at=NULL',
        'version': 'object_version=NULL', "expired": "expires_at=clock_timestamp()-interval '1 second'",
        'role': "output_role='preview_presentation'", 'media': "media_type='application/octet-stream'",
    }
    if mutation not in initial:
        database.execute('UPDATE photo.artifacts SET '+changes[mutation]+' WHERE id=%s', (refs[role],))
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with database.transaction():
            read(database,args)


def test_background_cleanup_sidecars_reject_changed_presentation_bytes(database):
    args, _ = sidecar_fixture(database)
    changed = (*args[:-1], args[-1]+' ')
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with database.transaction():
            read(database,changed)


@pytest.mark.parametrize('mutation', ['extra','missing_sha','numeric_sha','invalid_uuid','null','scalar','alias'])
def test_background_cleanup_sidecars_require_closed_reference_objects(database, mutation):
    def mutate(body):
        ref = body['before']
        if mutation == 'extra':
            ref['object_key'] = 'caller-controlled'
        elif mutation == 'missing_sha':
            del ref['sha256']
        elif mutation == 'numeric_sha':
            ref['sha256'] = 123
        elif mutation == 'invalid_uuid':
            ref['artifact_id'] = 'invalid'
        elif mutation == 'null':
            body['before'] = None
        elif mutation == 'scalar':
            body['before'] = 'invalid'
        elif mutation == 'alias':
            body['before'] = body['after']
    args, refs = sidecar_fixture(database, body_mutator=mutate)
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with database.transaction():
            read(database,args)


@pytest.mark.parametrize('mutation', ['extra','numeric_icc','numeric_srgb','width_string','width_zero','width_oversized','missing_resize','null_params'])
def test_background_cleanup_sidecars_require_closed_presentation_schema(database, mutation):
    def mutate(body):
        if mutation == 'extra':
            body['caller_override'] = True
        elif mutation == 'numeric_icc':
            body['icc_lock_sha'] = int('1'*64)
        elif mutation == 'numeric_srgb':
            body['srgb_sha'] = int('1'*64)
        elif mutation == 'width_string':
            body['width'] = '100'
        elif mutation == 'width_zero':
            body['width'] = 0
        elif mutation == 'width_oversized':
            body['width'] = 2561
        elif mutation == 'missing_resize':
            del body['resize']
        elif mutation == 'null_params':
            body['params'] = None
    args, refs = sidecar_fixture(database, body_mutator=mutate)
    with pytest.raises(psycopg.errors.InvalidParameterValue):
        with database.transaction():
            read(database,args)
