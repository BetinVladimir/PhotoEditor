"""Independent fixture pin for destructive learning-consent restore probes.

This executes observations only. Generic batch refusal and untested concurrent
revocation do not qualify a successful restore acceptance report.
"""
from dataclasses import asdict
from hashlib import sha256
import re
from photo_worker.contracts.base import canonical_bytes
from .restore_report import _json
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_revoke_runner import RestoreRevokeBatchInstallation,run_fixture_revoke_batch_lifecycle
from .restore_authority import require_restore_fixture


def revoke_fixture_descriptor(replay,revoke_target,document_target,positive,negative,binding,installation):
    """Public fixture identity without credentials, secret keys or source bytes."""
    def key_pin(value):return sha256(value.encode()).hexdigest()
    return _json({'schema_version':'restore-revoke-fixture/v1','database':replay.database,
        'run_id':replay.run_id,'build_sha256':replay.build_sha256,
        'recovery_sha256':replay.recovery_sha256,'replay_sha256':replay.replay_sha256,
        'fixture':asdict(revoke_target.fixture),'origin':revoke_target.origin,
        'document_id':document_target.document_id,'document_sha256':document_target.document_sha256,
        'access_path_sha256':key_pin(document_target.access.denied_path),
        'control_path_sha256':key_pin(document_target.access.control_path),
        'control_sha256':document_target.access.control_sha256,
        'access_denial':[document_target.access.expected_status,document_target.access.expected_code],
        'asset_id':positive.asset_id,'revision':positive.revision,'binding':asdict(binding),
        'revoke_key_sha256':key_pin(revoke_target.idempotency_key),
        'freeze_keys_sha256':[key_pin(positive.idempotency_key),key_pin(negative.idempotency_key)],
        'snapshot_id':installation.positive.snapshot_id,'policy_sha256':installation.positive.policy_sha256,
        'batch_keys_sha256':[key_pin(installation.positive.idempotency_key),key_pin(installation.negative.idempotency_key)],
        'session_id':installation.session_id,'maximum_seconds':installation.maximum_seconds,
        'source_sha256':sha256(installation.source_raw).hexdigest()})


def run_pinned_revoke_observations(replay,fixture_sha256,connection,client,buckets,revoke_target,document_target,
                                  positive,negative,binding,*,batch_installation,tls_context,
                                  assert_installed_fixture,check_active):
    """Reject a mismatched independently supplied fixture pin before mutation.

    Never retry the whole lifecycle after interruption: consent may already have
    committed. This entry point does not create a report with all checks true.
    """
    if (not isinstance(replay,VerifiedRestoreReplay)
            or not isinstance(batch_installation,RestoreRevokeBatchInstallation)
            or not isinstance(fixture_sha256,str) or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_REVOKE_INSTALLATION')
    descriptor=revoke_fixture_descriptor(replay,revoke_target,document_target,positive,negative,binding,batch_installation)
    if sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_REVOKE_FIXTURE_PIN')
    require_restore_fixture(check_active,assert_installed_fixture)
    return run_fixture_revoke_batch_lifecycle(connection,client,buckets,revoke_target,document_target,
        positive,negative,binding,batch_installation=batch_installation,expected_database=replay.database,
        tls_context=tls_context,assert_installed_fixture=assert_installed_fixture,check_active=check_active)
