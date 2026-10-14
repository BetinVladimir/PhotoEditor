"""Independent pin for the complete native legacy export observation fixture."""
from dataclasses import asdict
from hashlib import sha256
from pathlib import Path
import re
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export import EncodeParams
from .restore_apply_installation import apply_fixture_descriptor
from .restore_legacy_apply import RestoreLegacyApplyTicket
from .restore_export import RestoreExportTarget,export_request_body
from .restore_export_preflight import RestoreExportSelection
from .restore_export_lifecycle import run_fixture_export
from .restore_report import _json
from .restore_authority import require_restore_fixture


def export_fixture_descriptor(replay,target,binding,selection,apply_target,apply_ticket,download_target,*,
        params,intent_key,scratch_root,maximum_seconds,alpha_dtype='uint16'):
    if (not isinstance(target,RestoreExportTarget) or not isinstance(selection,RestoreExportSelection)
            or not isinstance(apply_ticket,RestoreLegacyApplyTicket) or not isinstance(params,EncodeParams)
            or target.tenant_id!=apply_target.tenant_id or target.origin!=apply_target.origin
            or target.authorization!=apply_target.authorization
            or not isinstance(intent_key,str) or not 1<=len(intent_key)<=128
            or any(not 33<=ord(c)<=126 for c in intent_key) or intent_key==target.idempotency_key
            or any(not isinstance(v,str) or re.fullmatch('[0-9a-f]{64}',v) is None
                for v in (selection.snapshot_digest,selection.preset_sha256,apply_ticket.request_sha256,apply_ticket.response_sha256))
            or type(selection.preset_revision) is not int or selection.preset_revision<1):
        raise ValueError('RESTORE_EXPORT_INSTALLATION')
    root=Path(scratch_root)
    if not root.is_absolute() or '..' in root.parts:raise ValueError('RESTORE_EXPORT_INSTALLATION')
    applied=apply_fixture_descriptor(replay,apply_target,binding,download_target,
        maximum_seconds=maximum_seconds,alpha_dtype=alpha_dtype)
    return _json({'schema_version':'restore-export-fixture/v1','apply_fixture':applied,
        'apply_ticket':asdict(apply_ticket),'selection_snapshot_id':target.selection_snapshot_id,
        'preset_id':target.preset_id,'selection':asdict(selection),
        'request_sha256':sha256(export_request_body(target)).hexdigest(),
        'request_key_sha256':sha256(target.idempotency_key.encode()).hexdigest(),
        'intent_key_sha256':sha256(intent_key.encode()).hexdigest(),
        'encode_params_sha256':sha256(canonical_bytes(params.model_dump(mode='json',exclude_none=True))).hexdigest(),
        'scratch_root':str(root),'storage_addressing':'path-style'})


def run_pinned_export(replay,fixture_sha256,connection,client,buckets,target,binding,selection,apply_target,
        apply_ticket,download_target,*,params,intent_key,scratch_root,maximum_seconds,pepper,tls_context,
        assert_installed_fixture,check_active,alpha_dtype='uint16'):
    if (not isinstance(fixture_sha256,str) or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_EXPORT_INSTALLATION')
    descriptor=export_fixture_descriptor(replay,target,binding,selection,apply_target,apply_ticket,download_target,
        params=params,intent_key=intent_key,scratch_root=scratch_root,maximum_seconds=maximum_seconds,alpha_dtype=alpha_dtype)
    if sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_EXPORT_FIXTURE_PIN')
    require_restore_fixture(check_active,assert_installed_fixture)
    return run_fixture_export(connection,client,buckets,target,binding,selection,apply_target,apply_ticket,download_target,
        expected_database=replay.database,params=params,intent_key=intent_key,scratch_root=scratch_root,
        maximum_seconds=maximum_seconds,pepper=pepper,tls_context=tls_context,
        assert_installed_fixture=assert_installed_fixture,check_active=check_active,alpha_dtype=alpha_dtype)
