"""Install all four concrete restore probes before the first fixture mutation."""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
import os

from photo_worker.contracts.base import canonical_bytes
from .restore_acceptance import PROBE_CHECKS,run_restore_acceptance_and_report,_require_restore_guard
from .restore_legacy_full_probe import (
    RestoreLegacyExportPlan,legacy_complete_fixture_descriptor,run_legacy_apply_probe)
from .restore_legacy_lifecycle import RestoreLegacyInstallation
from .restore_v2_workflow_runner import (
    v2_workflow_fixture_descriptor,run_fixture_v2_workflow_report)
from .restore_purge_runner import purge_fixture_descriptor,run_purge_negative_probe
from .restore_revoke_report_runner import (
    RestoreAdmissionRevokeInstallation,combined_revoke_descriptor,run_revoke_negative_probe)
from .restore_probe_report import validate_probe_report_root
from .restore_replay_receipt import VerifiedRestoreReplay


@dataclass(frozen=True)
class RestoreInstalledLegacyProbe:
    connection: object
    client: object
    buckets: tuple[str,...]
    installation: RestoreLegacyInstallation
    plan: RestoreLegacyExportPlan
    srgb_profile: bytes
    icc_registry: object
    pepper: bytes
    tls_context: object
    maximum_seconds: int
    assert_installed_fixture: object


@dataclass(frozen=True)
class RestoreInstalledV2Probe:
    connection: object
    client: object
    buckets: tuple[str,...]
    target: object
    binding: object
    frozen: object
    source_raw: bytes
    download_target: object
    session_id: object
    srgb_profile: bytes
    tls_context: object
    maximum_seconds: int
    assert_installed_fixture: object


@dataclass(frozen=True)
class RestoreInstalledPurgeProbe:
    connection: object
    client: object
    buckets: tuple[str,...]
    references: tuple
    deletion_target: object
    api_target: object
    api_sha256: str
    s3_target: object
    tls_context: object
    maximum_seconds: int
    assert_installed_fixture: object


@dataclass(frozen=True)
class RestoreInstalledRevokeProbe:
    connection: object
    client: object
    buckets: tuple[str,...]
    admission: RestoreAdmissionRevokeInstallation
    running_target: object
    child_run_id: object
    maximum_seconds: int
    renderer_barrier: object
    tls_context: object
    assert_installed_fixture: object


@dataclass(frozen=True)
class RestoreProbeDispatch:
    report_root: Path
    legacy: RestoreInstalledLegacyProbe
    v2: RestoreInstalledV2Probe
    purge: RestoreInstalledPurgeProbe
    revoke: RestoreInstalledRevokeProbe

    @staticmethod
    def _validate_scope(descriptors):
        try:
            purge_tenant=descriptors['purge_negative']['tenant_id']
            other_tenants=(
                descriptors['legacy_apply']['legacy']['preview']['tenant_id'],
                descriptors['legacy_apply']['legacy']['apply']['tenant_id'],
                descriptors['v2_workflow']['tenant_id'],
                descriptors['revoke_negative']['admission']['fixture']['tenant_id'],
                descriptors['revoke_negative']['running']['fixture']['tenant_id'])
        except (KeyError,TypeError):
            raise ValueError('RESTORE_PROBE_DISPATCH_SCOPE') from None
        if (not isinstance(purge_tenant,str) or not purge_tenant
                or any(not isinstance(tenant,str) or not tenant or tenant==purge_tenant
                       for tenant in other_tenants)):
            raise ValueError('RESTORE_PROBE_DISPATCH_SCOPE')

    def descriptors(self,replay):
        if not isinstance(replay,VerifiedRestoreReplay):
            raise ValueError('RESTORE_PROBE_DISPATCH_REPLAY')
        l,v,p,r=self.legacy,self.v2,self.purge,self.revoke
        return {
            'legacy_apply':legacy_complete_fixture_descriptor(replay,l.buckets,l.installation,
                l.plan,l.srgb_profile,l.pepper,l.maximum_seconds),
            'v2_workflow':v2_workflow_fixture_descriptor(replay,v.buckets,v.target,v.binding,
                v.frozen,v.download_target,v.session_id,v.srgb_profile,v.maximum_seconds),
            'purge_negative':purge_fixture_descriptor(replay,p.buckets,p.references,
                p.deletion_target,p.api_target,p.api_sha256,p.s3_target),
            'revoke_negative':combined_revoke_descriptor(replay,r.admission,r.running_target,
                r.child_run_id,r.maximum_seconds)}

    def validate_all(self,replay,pins,guard):
        if (not callable(guard) or not isinstance(pins,dict)
                or set(pins)!={kind for kind,_ in PROBE_CHECKS}):
            raise ValueError('RESTORE_PROBE_DISPATCH_INSTALLATION')
        _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
        validate_probe_report_root(self.report_root)
        for installed in (self.legacy,self.v2,self.purge,self.revoke):
            if not callable(installed.assert_installed_fixture):
                raise ValueError('RESTORE_PROBE_DISPATCH_FIXTURE_GUARD')
            _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
            _require_restore_guard(installed.assert_installed_fixture,'RESTORE_PROBE_DISPATCH_FIXTURE_GUARD')
            _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
        _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
        descriptors=self.descriptors(replay)
        self._validate_scope(descriptors)
        for kind,_ in PROBE_CHECKS:
            _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
            if sha256(canonical_bytes(descriptors[kind])).hexdigest()!=pins[kind]:
                raise ValueError('RESTORE_PROBE_DISPATCH_PIN')
            stem=str(replay.run_id)+'.'+kind
            if any(os.path.lexists(Path(self.report_root)/(stem+suffix))
                   for suffix in ('.pending','.json')):
                raise ValueError('RESTORE_PROBE_DISPATCH_ALREADY_REPORTED')
        _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
        stem=str(replay.run_id)+'.acceptance'
        if any(os.path.lexists(Path(self.report_root)/(stem+suffix))
               for suffix in ('.pending','.json')):
            raise ValueError('RESTORE_PROBE_DISPATCH_ALREADY_ACCEPTED')
        _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
        return descriptors

    def runner(self,pins):
        """Return the exact run_probe callback expected by run_restore_acceptance."""
        if not isinstance(pins,dict):raise ValueError('RESTORE_PROBE_DISPATCH_INSTALLATION')
        expected=dict(pins);validated=[];position=[0]
        def run(kind,replay,pin,guard):
            if not validated:
                self.validate_all(replay,expected,guard)
                validated.append((replay.run_id,replay.database,replay.build_sha256))
            required=tuple(name for name,_ in PROBE_CHECKS)
            if (validated[0]!=(replay.run_id,replay.database,replay.build_sha256)
                    or position[0]>=len(required) or kind!=required[position[0]]
                    or expected[kind]!=pin or pins!=expected):
                raise ValueError('RESTORE_PROBE_DISPATCH_CHANGED')
            _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
            descriptors=self.descriptors(replay)
            self._validate_scope(descriptors)
            if any(sha256(canonical_bytes(descriptors[name])).hexdigest()!=expected[name]
                   for name in required):
                raise ValueError('RESTORE_PROBE_DISPATCH_CHANGED')
            position[0]+=1
            root=self.report_root
            _require_restore_guard(guard,'RESTORE_PROBE_DISPATCH_INACTIVE')
            if kind=='legacy_apply':
                c=self.legacy
                return run_legacy_apply_probe(replay,pin,report_root=root,
                    connection=c.connection,client=c.client,buckets=c.buckets,
                    installation=c.installation,plan=c.plan,srgb_profile=c.srgb_profile,
                    icc_registry=c.icc_registry,pepper=c.pepper,tls_context=c.tls_context,
                    maximum_seconds=c.maximum_seconds,
                    assert_installed_fixture=c.assert_installed_fixture,check_active=guard)
            if kind=='v2_workflow':
                c=self.v2
                return run_fixture_v2_workflow_report(root,replay,pin,c.connection,c.client,
                    c.buckets,c.target,c.binding,c.frozen,c.source_raw,c.download_target,
                    session_id=c.session_id,srgb_profile=c.srgb_profile,tls_context=c.tls_context,
                    maximum_seconds=c.maximum_seconds,
                    assert_installed_fixture=c.assert_installed_fixture,check_active=guard)
            if kind=='purge_negative':
                c=self.purge
                return run_purge_negative_probe(replay,pin,report_root=root,
                    connection=c.connection,client=c.client,buckets=c.buckets,
                    references=c.references,deletion_target=c.deletion_target,
                    api_target=c.api_target,api_sha256=c.api_sha256,s3_target=c.s3_target,
                    tls_context=c.tls_context,maximum_seconds=c.maximum_seconds,
                    assert_installed_fixture=c.assert_installed_fixture,check_active=guard)
            if kind=='revoke_negative':
                c=self.revoke
                return run_revoke_negative_probe(replay,pin,report_root=root,
                    connection=c.connection,client=c.client,buckets=c.buckets,
                    admission=c.admission,running_target=c.running_target,
                    child_run_id=c.child_run_id,maximum_seconds=c.maximum_seconds,
                    renderer_barrier=c.renderer_barrier,tls_context=c.tls_context,
                    assert_installed_fixture=c.assert_installed_fixture,check_active=guard)
            raise ValueError('RESTORE_PROBE_DISPATCH_KIND')
        return run


def run_installed_restore_acceptance(dispatch,recovery_path,recovery_sha256,
                                     replay_path,replay_sha256,*,evidence,build_sha256,
                                     fixture_pins,assert_probe_isolation,
                                     verify_current_authorities,check_active):
    """Run fixed concrete probes and persist acceptance without writer cutover."""
    if not isinstance(dispatch,RestoreProbeDispatch):
        raise ValueError('RESTORE_PROBE_DISPATCH_INSTALLATION')
    return run_restore_acceptance_and_report(dispatch.report_root,recovery_path,
        recovery_sha256,replay_path,replay_sha256,evidence=evidence,
        build_sha256=build_sha256,fixture_pins=fixture_pins,
        run_probe=dispatch.runner(fixture_pins),
        assert_probe_isolation=assert_probe_isolation,
        verify_current_authorities=verify_current_authorities,
        check_active=check_active)
