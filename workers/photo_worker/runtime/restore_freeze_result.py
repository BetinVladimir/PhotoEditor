"""Read registered frozen recipe from its exact physical object version."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime.restore_objects import MinioRestoreObjects
from photo_worker.runtime.restore_authority import require_restore_fixture
from photo_worker.runtime.restore_revoke_freeze import (
    RestoreFreezeDenialTarget, RestoreFreezeBaselineBinding, RestoreFreezeBaselineEvidence, _freeze_document_id)
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture
from photo_worker.v2.render_recipe import decode_frozen_render_recipe
import psycopg
import re


@dataclass(frozen=True)
class RestoreFrozenResultEvidence:
    schema_version: str
    database: str
    result: RestoreFreezeBaselineEvidence
    object: RestoreObject
    authored_sha256: str


def verify_fixture_frozen_result(connection, client, buckets, target, binding, fixture, result,
                                 authored_sha256, *, expected_database, assert_fixture_scope, check_active):
    if (not isinstance(target, RestoreFreezeDenialTarget) or not isinstance(binding, RestoreFreezeBaselineBinding)
            or not isinstance(fixture, RestoreRevokeFixture) or not isinstance(result, RestoreFreezeBaselineEvidence)
            or fixture.tenant_id != target.tenant_id or fixture.actor_id != binding.actor_id
            or result.frozen_document_id != _freeze_document_id(target, binding.actor_id)
            or not isinstance(authored_sha256, str) or re.fullmatch('[0-9a-f]{64}', authored_sha256) is None
            or not isinstance(expected_database, str) or not 1 <= len(expected_database.encode()) <= 63
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_FROZEN_RESULT_INSTALLATION')
    def guard():
        require_restore_fixture(check_active, assert_fixture_scope, target, binding, fixture)
    def catalogue():
        guard()
        if connection.info.transaction_status != psycopg.pq.TransactionStatus.IDLE:
            raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
        with connection.transaction():
            connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            connection.execute("SET LOCAL statement_timeout='5s'")
            connection.execute('SET LOCAL row_security=off')
            row = connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if row != (expected_database, True): raise ValueError('RESTORE_FROZEN_RESULT_AUTHORITY')
            row = connection.execute('''SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
 FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id AND a.sha256=u.sha256
 JOIN photo.asset_versions v ON v.tenant_id=u.tenant_id AND v.id=u.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id AND asset.project_id=u.project_id
 WHERE u.tenant_id=%s AND u.project_id=%s AND u.document_id=%s AND u.artifact_id=%s AND u.sha256=%s
 AND u.asset_version_id=%s AND asset.id=%s AND asset.deleted_at IS NULL
 AND u.render_source_id=%s AND u.render_source_sha=%s AND u.render_draft_document_id=%s
 AND u.render_draft_revision=%s AND u.render_authored_sha=%s AND u.render_learning_checked
 AND u.render_learning->>'version_id'=%s AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())''',
                (fixture.tenant_id, fixture.project_id, result.frozen_document_id, result.recipe_artifact_id,
                 result.recipe_sha256, binding.asset_version_id, target.asset_id, binding.source_artifact_id,
                 binding.source_sha256, target.document_id, target.revision, authored_sha256, str(fixture.learning_version_id))).fetchone()
            if not row or len(row) != 5: raise ValueError('RESTORE_FROZEN_RESULT_REGISTRATION')
            ref = RestoreObject(*row)
            if (type(ref.size) is not int or not 2 <= ref.size <= 1048576
                    or ref.sha256 != result.recipe_sha256 or not isinstance(ref.key, str)
                    or not ref.key.startswith('tenants/' + str(fixture.tenant_id) + '/')):
                raise ValueError('RESTORE_FROZEN_RESULT_OBJECT')
            guard()
        return ref
    ref = catalogue()
    store = MinioRestoreObjects(client, buckets, check_active=guard)
    with store.open_version(ref) as stream:
        parts = []; size = 0
        while True:
            data = stream.read(min(65536, ref.size - size + 1))
            if not data: break
            size += len(data)
            if size > ref.size: raise ValueError('RESTORE_FROZEN_RESULT_SIZE')
            parts.append(data)
    raw = b''.join(parts)
    if len(raw) != ref.size or sha256(raw).hexdigest() != ref.sha256:
        raise ValueError('RESTORE_FROZEN_RESULT_SHA')
    frozen = decode_frozen_render_recipe(raw)
    source = frozen.recipe.source
    if (frozen.recipe_sha256 != authored_sha256 or frozen.recipe.learning_version_id != fixture.learning_version_id
            or frozen.learning is None or frozen.learning.version_id != fixture.learning_version_id
            or source.asset_version_id != binding.asset_version_id or source.source.artifact_id != binding.source_artifact_id
            or source.source.sha256 != binding.source_sha256 or source.canvas_hash != binding.canvas_hash):
        raise ValueError('RESTORE_FROZEN_RESULT_BINDING')
    if catalogue() != ref: raise ValueError('RESTORE_FROZEN_RESULT_CHANGED')
    guard()
    return RestoreFrozenResultEvidence('restore-frozen-result/v1', expected_database, result, ref, authored_sha256)
