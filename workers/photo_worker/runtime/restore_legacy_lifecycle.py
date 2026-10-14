"""Combined independently pinned preview/Apply observations, not acceptance."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from .restore_preview_installation import preview_fixture_descriptor,run_pinned_preview
from .restore_apply_installation import apply_fixture_descriptor,run_pinned_legacy_apply
from .restore_legacy_preview_result import observe_fixture_legacy_preview_result
from .restore_legacy_result import observe_fixture_legacy_apply_result
from .restore_legacy_apply import validate_fixture_legacy_apply
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreLegacyInstallation:
    preview_target: object
    apply_target: object
    binding: object
    download_target: object
    source_width: int
    source_height: int
    maximum_seconds: int
    alpha_dtype: str='uint16'


@dataclass(frozen=True)
class RestoreLegacyObservations:
    fixture_sha256: str
    preview: object
    apply: object


def legacy_fixture_descriptor(replay,installation,srgb_profile):
    if not isinstance(installation,RestoreLegacyInstallation):raise ValueError('RESTORE_LEGACY_INSTALLATION')
    i=installation
    if (i.preview_target.tenant_id!=i.apply_target.tenant_id or i.preview_target.origin!=i.apply_target.origin
            or i.preview_target.authorization!=i.apply_target.authorization
            or i.preview_target.idempotency_key==i.apply_target.idempotency_key):
        raise ValueError('RESTORE_LEGACY_INSTALLATION_SCOPE')
    return {'schema_version':'restore-legacy-fixture/v1',
        'preview':preview_fixture_descriptor(replay,i.preview_target,i.binding,i.download_target,
            source_width=i.source_width,source_height=i.source_height,srgb_profile=srgb_profile,
            maximum_seconds=i.maximum_seconds,alpha_dtype=i.alpha_dtype),
        'apply':apply_fixture_descriptor(replay,i.apply_target,i.binding,i.download_target,
            maximum_seconds=i.maximum_seconds,alpha_dtype=i.alpha_dtype)}


def run_pinned_legacy_observations(replay,fixture_sha256,connection,client,buckets,installation,*,srgb_profile,
        icc_registry,tls_context,assert_installed_fixture,check_active):
    """Never retry whole runner; preview may commit before Apply fails.

    WorkingImage document delivery is not encoded final-export qualification.
    This runner deliberately cannot publish all-true legacy acceptance checks.
    """
    descriptor=legacy_fixture_descriptor(replay,installation,srgb_profile)
    if sha256(canonical_bytes(descriptor)).hexdigest()!=fixture_sha256:
        raise ValueError('RESTORE_LEGACY_FIXTURE_PIN')
    i=installation
    common=dict(tls_context=tls_context,assert_installed_fixture=assert_installed_fixture,check_active=check_active)
    validate_fixture_legacy_apply(i.apply_target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    def scope(*_):require_restore_fixture(check_active,assert_installed_fixture)
    scope()
    preview=run_pinned_preview(replay,sha256(canonical_bytes(descriptor['preview'])).hexdigest(),
        connection,client,buckets,i.preview_target,i.binding,i.download_target,
        source_width=i.source_width,source_height=i.source_height,srgb_profile=srgb_profile,
        maximum_seconds=i.maximum_seconds,alpha_dtype=i.alpha_dtype,icc_registry=icc_registry,**common)
    applied=run_pinned_legacy_apply(replay,sha256(canonical_bytes(descriptor['apply'])).hexdigest(),
        connection,client,buckets,i.apply_target,i.binding,i.download_target,
        maximum_seconds=i.maximum_seconds,alpha_dtype=i.alpha_dtype,**common)
    for observe,target,ticket,expected in (
            (observe_fixture_legacy_preview_result,i.preview_target,preview.ticket,preview.result),
            (observe_fixture_legacy_apply_result,i.apply_target,applied.ticket,applied.result)):
        current=observe(connection,target,i.binding,ticket,expected_database=replay.database,
            assert_fixture_scope=scope,check_active=check_active)
        if current!=expected:raise ValueError('RESTORE_LEGACY_LIFECYCLE_CHANGED')
    scope();return RestoreLegacyObservations(fixture_sha256,preview,applied)
