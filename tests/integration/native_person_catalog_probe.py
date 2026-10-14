"""Canonical worker result to API catalogue, using synthetic verified metadata.

This does not assert physical object bytes, model inference or admission quality.
"""
from uuid import uuid4, UUID
from psycopg.types.json import Jsonb


def verify_person_canonical_catalog(db, case, packet, task, outputs, expected):
    tenant, project, actor, session = [case[k] for k in ('Tenant', 'Project', 'Actor', 'Session')]
    binding = packet['binding']; source = binding['source']
    root, child = outputs[2], outputs[0]
    args = (tenant, project, actor, session, binding['asset_version_id'], source['artifact_id'], source['sha256'])

    def candidate(values=args, profile=expected):
        db.execute('SET LOCAL ROLE photo_api')
        try:
            return db.execute('SELECT * FROM photo.semantic_proposal_person_candidates(%s,%s,%s,%s,%s,%s,%s,%s)', (*values, Jsonb(profile))).fetchall()
        finally:
            db.execute('RESET ROLE')

    wanted = [(UUID(root['artifact_id']), root['sha256'], UUID(child['artifact_id']), child['sha256'])]
    assert candidate() == wanted
    db.execute('SET LOCAL ROLE photo_api')
    try:
        evidence = db.execute('SELECT contract,artifact_id,sha256 FROM photo.semantic_proposal_person_evidence_objects(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', (*args, root['artifact_id'], root['sha256'], child['artifact_id'], child['sha256'])).fetchall()
    finally:
        db.execute('RESET ROLE')
    params = packet['registration']['params']
    assert evidence == [('PersonParams', UUID(params['artifact_id']), params['sha256']), ('PersonReport', UUID(outputs[3]['artifact_id']), outputs[3]['sha256'])]
    for i in range(6):
        changed = list(args); changed[i] = str(uuid4())
        assert not candidate(tuple(changed)), i
    assert not candidate((*args[:-1], '0' * 64))
    for field in ('ModelLockSHA', 'RuntimeLockSHA', 'DeviceProfileSHA'):
        assert not candidate(profile=dict(expected, **{field: '0' * 64})), field
    assert not candidate(profile=dict(expected, Backend='cpu'))
    for query, values in (
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (session,)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=%s AND id=%s', (tenant, project)),
        ("UPDATE photo.asset_versions SET review_state='pending' WHERE tenant_id=%s AND id=%s", (tenant, binding['asset_version_id'])),
        ('UPDATE photo.task_attempts SET finished_at=NULL WHERE tenant_id=%s AND task_id=%s', (tenant, task)),
        ("UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{runtime_lock_sha}',to_jsonb(%s::text)) WHERE tenant_id=%s AND task_id=%s", ('0' * 64, tenant, task)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s", (tenant, root['artifact_id'])),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s', (tenant, child['artifact_id'])),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query, values)
            assert not candidate(), query
        assert candidate() == wanted
