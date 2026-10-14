"""Compose running CPU assembly catalog and exact object-byte authorities."""
from copy import deepcopy
from types import SimpleNamespace
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import V2SemanticAssembleParams
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef as AuthorityRef
from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
from photo_worker.v2.semantic_qualification_loader import semantic_qualification_loader
from .assemble_adapter import SemanticAssemblyInstallation
from .assembly_snapshot import sql_semantic_assembly_snapshot
from .assembly_producers import sql_semantic_assembly_provenance


def sql_semantic_assembly_factory(*,database,objects,bucket):
    required=('semantic_assembly_qualification','semantic_assembly_report_evidence','semantic_assembly_snapshot','semantic_assembly_producer')
    if (any(not callable(getattr(database,name,None)) for name in required)
            or not callable(getattr(objects,'get_object',None)) or not isinstance(bucket,str) or not 3<=len(bucket)<=63):
        raise ValueError('SEMANTIC_ASSEMBLY_AUTHORITY_REQUIRED')
    def install(ctx,artifacts):
        lease=deepcopy(ctx.lease)
        def active():
            ctx.check_active()
            if ctx.lease!=lease:raise AdoptionInvalid('SEMANTIC_LEASE_CHANGED')
        active()
        pin=SemanticQualificationPin.from_value(database.semantic_assembly_qualification(deepcopy(lease)))
        params=V2SemanticAssembleParams.model_validate_json(artifacts.contract_bytes(lease.command.params,V2SemanticAssembleParams))
        pin.bind(params.model_lock_sha,AuthorityRef(params.calibration.artifact_id,params.calibration.sha256))
        # GPU transfer evidence is verified for CPU assembly; no claim that this
        # CPU host matches the transfer device/runtime measurements.
        report=semantic_qualification_loader(SimpleNamespace(semantic_report_evidence=database.semantic_assembly_report_evidence),lease,objects,bucket,frozen_pin=pin,check_active=active)
        report()
        snapshot=sql_semantic_assembly_snapshot(database,ctx)
        producers=sql_semantic_assembly_provenance(database,objects,bucket,ctx)
        def load_snapshot(reader,intent):
            if reader is not artifacts:raise AdoptionInvalid('SEMANTIC_ARTIFACT_OWNER_CHANGED')
            report();result=snapshot(reader,intent);report();return result
        def verify(assembly,intent,frozen_snapshot,candidates):
            if canonical_bytes(assembly)!=canonical_bytes(params):raise AdoptionInvalid('SEMANTIC_PARAMS_CHANGED')
            active();report();producers(assembly,intent,frozen_snapshot,candidates);report();active()
        return SemanticAssemblyInstallation(load_snapshot,verify)
    return install
