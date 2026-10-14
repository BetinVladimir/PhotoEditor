"""SQL-authorized donor execution result bridge; registry wiring is separate."""
from copy import deepcopy

from photo_worker.contracts.base import canonical_bytes, ArtifactRef
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.operations.portrait.donor_handoff import donor_operation_result
from .portrait_open_eyes_manual_stage import prepare_and_stage_sql_manual_open_eyes
from .portrait_donor_qualification_loader import load_donor_qualification
from .portrait_donor_qualification import DonorQualificationPin
from .portrait_donor_attempt import donor_attempt_checkpoint
from .portrait_donor_source import donor_source_authority
from .portrait_donor_manual_authority import donor_manual_authority


def execute_sql_manual_open_eyes(database,lease,params_raw,source_raw,donor_raw,
        confirmation_raw,source_faces_raw,donor_faces_raw,target_raw,protected_raw,
        root_ref,root_raw,producer_params_ref,producer_params_raw,reader,artifacts,scratch,*,
        qualification_objects,bucket,check_active,declared_inputs,output_budget,**kwargs):
    """Return review-only OperationResult after repeated current SQL authority.

    The caller supplies the verified full input index and operator runtime locks.
    TaskArtifacts owns staging/upload; this function does not install a registry
    handler or grant adoption permission.
    """
    if not callable(check_active):raise ValueError('DONOR_AUTHORITY')
    if type(output_budget) is not int or not 1<=output_budget<=9007199254740991:
        raise ValueError('DONOR_OUTPUT_BUDGET_INVALID')
    check_active()
    owned=deepcopy(lease)
    raw=(params_raw,source_raw,donor_raw,confirmation_raw,source_faces_raw,
         donor_faces_raw,target_raw,protected_raw,root_raw,producer_params_raw)
    for i,value in enumerate(raw):
        if not isinstance(value,(bytes,bytearray)) or not 2<=len(value)<=(65536 if i in (0,3) else 1<<20):
            raise ValueError('DONOR_DOCUMENT_INVALID')
    body,source,donor,proof,sfaces,dfaces,target,guard,root,producer=map(bytes,raw)
    root_pin=ArtifactRef.model_validate_json(canonical_bytes(root_ref))
    producer_pin=ArtifactRef.model_validate_json(canonical_bytes(producer_params_ref))
    if not isinstance(declared_inputs,(list,tuple)) or not 1<=len(declared_inputs)<=512:
        raise ValueError('DONOR_INPUTS_INVALID')
    inputs=tuple(ArtifactRef.model_validate_json(canonical_bytes(ref)) for ref in declared_inputs)
    if kwargs.get('vectors_raw') is not None:
        vectors=kwargs['vectors_raw']
        if not isinstance(vectors,(bytes,bytearray)) or not 2<=len(vectors)<=1<<20:
            raise ValueError('DONOR_DOCUMENT_INVALID')
    options=deepcopy(kwargs)
    staged=prepare_and_stage_sql_manual_open_eyes(database,owned,body,source,donor,proof,
        sfaces,dfaces,target,guard,root_pin,root,producer_pin,producer,reader,artifacts,scratch,
        qualification_objects=qualification_objects,bucket=bucket,check_active=check_active,**options)
    # Reload original qualification after staging, retaining its exact metadata
    # for both handoff checkpoints. No transaction spans object IO.
    _,_,_,current=load_donor_qualification(database,owned,body,proof,
        qualification_objects,bucket,check_active=check_active)
    def qualified():return DonorQualificationPin.from_value(current())
    attempt=donor_attempt_checkpoint(database,owned,body,qualified,check_active)
    source_authority=donor_source_authority(database,owned,body,attempt)
    manual=donor_manual_authority(database,owned,body,source,target,guard,
        root_pin,producer_pin,producer,attempt)
    params=PortraitOpenEyesParams.model_validate_json(body)
    def checkpoint():
        attempt()
        if source_authority(params.model_copy(deep=True),proof) is not True:
            raise ValueError('DONOR_AUTHORITY')
        if manual(params.model_copy(deep=True),root_pin.model_copy(deep=True),
                  producer_pin.model_copy(deep=True)) is not True:
            raise ValueError('DONOR_AUTHORITY')
        attempt()
    return donor_operation_result(staged,declared_inputs=inputs,
        output_budget=output_budget,check_active=checkpoint)


def execute_declared_sql_open_eyes(ctx,artifacts,database,reader=None,*,
        qualification_objects,bucket,**runtime_options):
    """Connect verified TaskArtifacts documents/index to SQL donor execution.

    The installed caller provides trusted runtime options. By default the
    worker SQL catalog supplies versioned input evidence. An injected reader
    must have equivalent current-attempt authority. No registry is installed.
    """
    from .portrait_donor_input_documents import load_donor_input_documents
    from .portrait_donor_reader import DonorInputReader
    forbidden={'declared_inputs','output_budget','vectors_raw','check_active'}
    if forbidden & runtime_options.keys():raise ValueError('DONOR_LEASE_INPUT_OVERRIDE')
    ctx.check_active()
    inputs=artifacts.declared_input_refs(512)
    documents=load_donor_input_documents(artifacts,ctx.lease.command.params,check_active=ctx.check_active)
    estimate=ctx.lease.execution_profile.get('resource_estimate')
    budget=estimate.get('output_bytes') if isinstance(estimate,dict) else None
    if reader is None:
        from .portrait_donor_object_reader import DonorWorkerObjectReader
        reader=DonorWorkerObjectReader(database,ctx.lease,documents.params,
            documents.confirmation,qualification_objects,bucket=bucket,check_active=ctx.check_active)
    bounded_reader=DonorInputReader(artifacts,reader,check_active=ctx.check_active)
    return execute_sql_manual_open_eyes(database,ctx.lease,documents.params,
        documents.source,documents.donor,documents.confirmation,documents.source_faces,
        documents.donor_faces,documents.target,documents.protected,documents.root_ref,
        documents.root,documents.producer_ref,documents.producer,bounded_reader,
        artifacts,artifacts.scratch,qualification_objects=qualification_objects,bucket=bucket,
        check_active=ctx.check_active,declared_inputs=inputs,output_budget=budget,
        vectors_raw=documents.vectors,**runtime_options)
