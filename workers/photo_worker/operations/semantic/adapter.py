"""Qualified V27 installer entry point for the shared worker dispatcher."""
from dataclasses import dataclass
from typing import Callable
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import Operation,OperationFailure
from .transfer import execute_transfer


@dataclass(frozen=True)
class SemanticTransferInstallation:
    """Per-lease dependencies supplied by trusted runtime/source qualification.

    No default verifier or inferred empty protection mask is installed. The
    factory must resolve pinned protection and source/scene/snapshot authority
    for this lease. Qualification and downstream admission are external gates.
    """
    load_arrays: Callable
    verify_runtime: Callable
    verify_scene: Callable

    def __post_init__(self):
        if not all(callable(value) for value in
                (self.load_arrays,self.verify_runtime,self.verify_scene)):
            raise ValueError('SEMANTIC_INSTALLATION_INVALID')


def _qualified_runtime(ctx,*,database,objects,bucket,frozen_pin,
        model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha):
    from photo_worker.v2.semantic_qualification_loader import semantic_qualification_loader
    from photo_worker.v2.semantic_qualification_pin import semantic_runtime_authority
    loader=semantic_qualification_loader(database,ctx.lease,objects,bucket,
        frozen_pin=frozen_pin,check_active=ctx.check_active)
    return semantic_runtime_authority(loader,frozen_pin=frozen_pin,
        model_lock_sha=model_lock_sha,runtime_lock_sha=runtime_lock_sha,
        device_profile_sha=device_profile_sha,dataset_sha=dataset_sha,check_active=ctx.check_active)


def qualified_semantic_transfer_installation(ctx,*,database,objects,bucket,frozen_pin,
        model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha,load_arrays,verify_scene):
    """Wire live leased report proof to explicit source/protection/scene owners."""
    if not callable(load_arrays) or not callable(verify_scene):
        raise ValueError('SEMANTIC_INSTALLATION_INVALID')
    verify=_qualified_runtime(ctx,database=database,objects=objects,bucket=bucket,
        frozen_pin=frozen_pin,model_lock_sha=model_lock_sha,runtime_lock_sha=runtime_lock_sha,
        device_profile_sha=device_profile_sha,dataset_sha=dataset_sha)
    return SemanticTransferInstallation(load_arrays,verify,verify_scene)


def qualified_sql_semantic_transfer_installation(ctx,*,database,objects,bucket,frozen_pin,
        model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha,params_bytes,
        verify_color,verify_scene,verify_protection=None,load_protection_document=None):
    """Compose exact report proof with frozen SQL source/protection authority.

    params_bytes must be the hash-verified leased Params object. Trusted color,
    Provide either a trusted protection verifier or a hash/version-verifying
    document reader; the latter installs live SQL canonical-origin verification.
    Color and planar scene verifiers remain explicit.
    Runtime qualification is checked before exposing any protection metadata.
    """
    from .load_inputs import sql_frozen_semantic_array_loader
    if load_protection_document is not None:
        if verify_protection is not None or not callable(load_protection_document):
            raise ValueError('SEMANTIC_INSTALLATION_INVALID')
        from .protection_authority import sql_semantic_protection_authority
        verify_protection=sql_semantic_protection_authority(database,ctx.lease,params_bytes,
            load_protection_document,check_active=ctx.check_active)
    if not all(callable(value) for value in (verify_color,verify_protection,verify_scene)):
        raise ValueError('SEMANTIC_INSTALLATION_INVALID')
    verify=_qualified_runtime(ctx,database=database,objects=objects,
        bucket=bucket,frozen_pin=frozen_pin,model_lock_sha=model_lock_sha,
        runtime_lock_sha=runtime_lock_sha,device_profile_sha=device_profile_sha,
        dataset_sha=dataset_sha)
    verify()
    arrays=sql_frozen_semantic_array_loader(database,ctx.lease,params_bytes,
        verify_color=verify_color,verify_protection=verify_protection,check_active=ctx.check_active)
    return SemanticTransferInstallation(arrays,verify,verify_scene)


def sql_semantic_artifact_factory(*,database,objects,bucket,model_lock_sha,runtime_lock_sha,
        device_profile_sha,dataset_sha,verify_color,verify_scene):
    """Build the gateway-session installer used by build_worker/run_worker.

    Measurements and scene/color verifiers must come from trusted deployment.
    Qualification, Params and protection are resolved for each live leased task.
    """
    from photo_worker.contracts.semantic import SemanticTransferParams
    from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
    import re
    if (not all(callable(value) for value in (verify_color,verify_scene))
            or not isinstance(bucket,str) or not bucket
            or not all(isinstance(value,str) and re.fullmatch('[0-9a-f]{64}',value)
                for value in (model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha))
            or not callable(getattr(objects,'get_object',None))
            or not all(callable(getattr(database,name,None)) for name in (
                'semantic_qualification','semantic_report_evidence','semantic_source_ready',
                'semantic_protection_pins','semantic_protection_origin'))):
        raise ValueError('SEMANTIC_INSTALLATION_INVALID')
    def install(ctx,artifacts):
        from copy import deepcopy
        from types import SimpleNamespace
        from photo_worker.v2.adoption import AdoptionInvalid
        owned_lease=deepcopy(ctx.lease)
        def active():
            ctx.check_active()
            if ctx.lease!=owned_lease:raise AdoptionInvalid('SEMANTIC_LEASE_CHANGED')
        active()
        pin=SemanticQualificationPin.from_value(database.semantic_qualification(deepcopy(owned_lease)))
        active()
        raw=artifacts.contract_bytes(deepcopy(owned_lease.command.params),SemanticTransferParams)
        active()
        return qualified_sql_semantic_transfer_installation(SimpleNamespace(lease=owned_lease,check_active=active),database=database,objects=objects,
            bucket=bucket,frozen_pin=pin,model_lock_sha=model_lock_sha,runtime_lock_sha=runtime_lock_sha,
            device_profile_sha=device_profile_sha,dataset_sha=dataset_sha,params_bytes=raw,
            verify_color=verify_color,verify_scene=verify_scene,load_protection_document=artifacts.contract)
    return install


def semantic_transfer_operations(config:ArtifactConfig,*,installation_factory=None,artifact_installation_factory=None)->list[Operation]:
    if ((installation_factory is None)==(artifact_installation_factory is None)
            or not callable(installation_factory if installation_factory is not None else artifact_installation_factory)):
        raise ValueError('SEMANTIC_INSTALLATION_REQUIRED')
    def run(ctx):
        ctx.check_active()
        profile=ctx.lease.execution_profile
        if profile.get('renderer_version')!='v2.2' or profile.get('backend')!='cuda':
            raise OperationFailure('CONFIG_INVALID')
        installation=installation_factory(ctx) if installation_factory is not None else None
        if installation_factory is not None and not isinstance(installation,SemanticTransferInstallation):
            raise OperationFailure('CONFIG_INVALID')
        ctx.check_active()
        with TaskArtifacts.create(ctx,config,frozenset({'document','mask','mask_pixels','transform'})) as artifacts:
            if artifact_installation_factory is not None:
                installation=artifact_installation_factory(ctx,artifacts)
                if not isinstance(installation,SemanticTransferInstallation):raise OperationFailure('CONFIG_INVALID')
                ctx.check_active()
            return execute_transfer(ctx,artifacts,load_arrays=installation.load_arrays,
                verify_runtime=installation.verify_runtime,verify_scene=installation.verify_scene)
    # Matching runs on CPU inside the normative GPU pool, where semantic
    # analysis/model qualification and all installed dependencies reside.
    return [Operation('semantic.transfer','gpu',run,version='1')]
