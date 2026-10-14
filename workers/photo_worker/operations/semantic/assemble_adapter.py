"""Leased SemanticPlan assembly with explicit scoped provenance installation."""
from dataclasses import dataclass
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import V2SemanticAssembleParams,EditIntent,TransferCandidate
from photo_worker.operations.session.contracts import Calibration,InputSnapshot
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation,OperationResult,OperationFailure
from .assemble import assemble_semantic_plan


def execute_assembly(ctx,artifacts,*,load_snapshot,verify_provenance):
    """Installer proves candidate producer pins and snapshot project authority.

    The snapshot loader uses the intent's frozen ID, never mutable selection.
    Candidate producer provenance must include exact intent, model/calibration,
    target binding and successful adopted attempt. Callbacks receive owned
    copies; their mutations cannot change the plan being assembled.
    """
    if not callable(load_snapshot) or not callable(verify_provenance):raise OperationFailure('CONFIG_INVALID')
    active=ctx.check_active;active();profile=deepcopy(ctx.lease.execution_profile)
    if profile.get('backend')!='cpu' or profile.get('renderer_version')!='v2.2':raise OperationFailure('CONFIG_INVALID')
    estimate=profile.get('resource_estimate')
    costs=dict(ram_bytes=256*1024*1024,scratch_bytes=32*1024*1024,vram_bytes=0,output_bytes=8*1024*1024)
    if not isinstance(estimate,dict) or any(type(estimate.get(k)) is not int or not 0<=estimate[k]<=2**53-1 for k in costs):
        raise OperationFailure('CONFIG_INVALID')
    for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('vram_bytes','VRAM_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[key]<costs[key]:raise OperationFailure(code)
    params=V2SemanticAssembleParams.model_validate_json(canonical_bytes(artifacts.params(V2SemanticAssembleParams)))
    def document(ref,model):
        active();return model.model_validate_json(canonical_bytes(artifacts.contract(ref,model)))
    intent=document(params.intent,EditIntent);calibration=document(params.calibration,Calibration)
    snapshot=InputSnapshot.model_validate_json(canonical_bytes(load_snapshot(artifacts,deepcopy(intent))))
    candidates=[];ctx.progress(0,len(params.candidates)+1)
    for n,ref in enumerate(params.candidates):
        candidates.append(document(ref,TransferCandidate));ctx.progress(n+1,len(params.candidates)+1)
    plan=assemble_semantic_plan(params,intent,snapshot,calibration,candidates,check_active=active)
    active();verify_provenance(deepcopy(params),deepcopy(intent),deepcopy(snapshot),deepcopy(candidates));active()
    root=artifacts.stage_contract(plan,'document');active();ctx.progress(len(params.candidates)+1,len(params.candidates)+1)
    return OperationResult(outputs=[root],review_required=True,bytes_written=len(canonical_bytes(plan)))


@dataclass(frozen=True)
class SemanticAssemblyInstallation:
    load_snapshot: object
    verify_provenance: object

    def __post_init__(self):
        if not callable(self.load_snapshot) or not callable(self.verify_provenance):
            raise ValueError('SEMANTIC_ASSEMBLY_AUTHORITY_REQUIRED')


def semantic_assembly_operations(config,*,load_snapshot=None,verify_provenance=None,installation_factory=None):
    if installation_factory is None:
        installation=SemanticAssemblyInstallation(load_snapshot,verify_provenance)
    elif not callable(installation_factory) or load_snapshot is not None or verify_provenance is not None:
        raise ValueError('SEMANTIC_ASSEMBLY_AUTHORITY_REQUIRED')
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'document'})) as artifacts:
            owned=installation if installation_factory is None else installation_factory(ctx,artifacts)
            if not isinstance(owned,SemanticAssemblyInstallation):raise OperationFailure('CONFIG_INVALID')
            return execute_assembly(ctx,artifacts,load_snapshot=owned.load_snapshot,verify_provenance=owned.verify_provenance)
    return [Operation('v2.semantic.assemble','cpu',run,version='1')]
