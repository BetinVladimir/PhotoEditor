"""A pending training draft retains exact bytes across a scheduler crash."""

from hashlib import sha256
import json
from uuid import uuid4, uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_v23_record_correction import _fixture


def _jcs(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def test_training_recovery_is_atomic_and_sha_pinned(db):
    tenant, actor, session, project, _, _ = _fixture(db)
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tenant,))
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    db.execute('SET LOCAL ROLE photo_api')
    profile = db.execute('SELECT photo.create_v23_profile(%s,%s,%s,%s,%s,%s,%s)',
                         (tenant, project, 'Recovery', 'photographer', None,
                          actor, session)).fetchone()[0]
    db.execute('RESET ROLE')
    key = 'recovery-' + uuid4().hex
    run = uuid5(tenant, f'V2.Admit:{actor}:{key}')
    workflow, snapshot = uuid5(run, 'workflow'), uuid5(run, 'training.snapshot')
    manifest = {'schema_version': 1, 'profile_id': str(profile), 'examples': []}
    manifest_jcs = _jcs(manifest).decode()
    manifest_sha = sha256(manifest_jcs.encode()).hexdigest()
    snapshot_body = {'schema_version': 1, 'project_id': str(project),
                     'snapshot_id': str(snapshot), 'items': [{
                         'ordinal': 0, 'artifact': {
                             'artifact_id': str(uuid5(run, 'v23_training_manifest')),
                             'sha256': manifest_sha},
                         'contract': 'TrainingManifest', 'owner_id': str(profile)}]}
    snapshot_digest = sha256(_jcs(snapshot_body)).hexdigest()
    snapshot_body['snapshot_digest'] = snapshot_digest
    snapshot_jcs = _jcs(snapshot_body).decode()
    snapshot_sha = sha256(snapshot_jcs.encode()).hexdigest()
    policy_jcs = _jcs({'schema_version': 1}).decode()
    policy_sha = sha256(policy_jcs.encode()).hexdigest()
    config = {'project_id': str(project), 'v23_training': {
        'schema_version': 1, 'profile_id': str(profile),
        'expected_revision': '1', 'manifest_sha': manifest_sha,
        'snapshot_digest': snapshot_digest, 'snapshot_body_sha': snapshot_sha}}
    recovery = {'schema_version': 1, 'manifest_jcs': manifest_jcs,
                'manifest_sha': manifest_sha, 'snapshot_jcs': snapshot_jcs,
                'snapshot_body_sha': snapshot_sha,
                'snapshot_digest': snapshot_digest,
                'policy_jcs': policy_jcs, 'policy_sha': policy_sha,
                'reserved_bytes': '4096', 'expected_revision': '1',
                'profile_id': str(profile), 'project_id': str(project)}
    request_sha = '7' * 64
    db.execute('SET LOCAL ROLE photo_scheduler')
    begin = 'SELECT * FROM photo.begin_v23_training_draft(' + ','.join(['%s'] * 13) + ')'
    args = (tenant, run, workflow, profile, 1, actor, session,
            policy_sha, '8' * 64, Jsonb(config), 4096, key, request_sha)
    save = 'SELECT photo.save_v23_training_recovery(' + ','.join(['%s'] * 7) + ')'
    probe = 'SELECT * FROM photo.v23_training_replay(' + ','.join(['%s'] * 6) + ')'
    with db.transaction():
        assert db.execute(begin, args).fetchone() == ('created', run, snapshot)
        assert db.execute(save, (tenant, actor, session, run, key,
                                 request_sha, Jsonb(recovery))).fetchone()[0] is True
    assert db.execute(probe, (tenant, actor, session, run, key,
                              request_sha)).fetchone() == (
                                  'pending', run, workflow, recovery)
    changed = dict(recovery, manifest_jcs=manifest_jcs + ' ')
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute(save, (tenant, actor, session, run, key,
                          request_sha, Jsonb(changed)))
    assert db.execute(probe, (tenant, actor, session, run, key,
                              '9' * 64)).fetchone()[0] == 'conflict'
