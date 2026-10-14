"""Declared SQL-backed negative execution; installation is separate."""
from copy import deepcopy
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.optical.negative_handoff import negative_operation_result
from .negative_qualification_loader import load_negative_qualification
from .negative_qualification import NegativeQualificationPin
from .negative_leased_stage import prepare_and_stage_leased_negative
from .negative_attempt import negative_attempt_checkpoint
from .negative_source import negative_source_authority
from .negative_reader import NegativeInputReader
from .negative_object_reader import NegativeWorkerObjectReader


def execute_declared_sql_negative(ctx,artifacts,database,reader=None,*,
        qualification_objects,bucket,runtime_lock,runtime_sha,source_max_bytes,
        ram_max_bytes,verify_color):
    ctx.check_active();lease=deepcopy(ctx.lease)
    command=lease.command
    if command.operation!='optical.negative' or command.operation_version!='1' or command.resource_class!='cpu':
        raise ValueError('NEGATIVE_OPERATION_INVALID')
    body=artifacts.contract_bytes(command.params,OpticalNegativeParams)
    params=OpticalNegativeParams.model_validate_json(body)
    source=artifacts.contract_bytes(params.base.source,WorkingImage)
    image=WorkingImage.model_validate_json(source)
    inputs=artifacts.declared_input_refs(512)
    report,calibration,policy,current=load_negative_qualification(database,lease,body,
        qualification_objects,bucket,input_icc_sha=image.color.icc_sha256,check_active=ctx.check_active)
    pin=NegativeQualificationPin.from_value(current())
    for ref in (pin.report,pin.calibration):
        artifacts.require_declared_input(ArtifactRef(artifact_id=ref.artifact_id,sha256=ref.sha256))
    def qualified():return NegativeQualificationPin.from_value(current())
    attempt=negative_attempt_checkpoint(database,lease,body,qualified,ctx.check_active)
    authority=negative_source_authority(database,lease,body,verify_color=verify_color,check_active=attempt)
    def checkpoint():
        attempt();authority(params,source);attempt()
    if reader is None:
        reader=NegativeWorkerObjectReader(database,lease,body,qualification_objects,
            bucket=bucket,check_active=checkpoint)
    bounded=NegativeInputReader(artifacts,reader,check_active=checkpoint)
    staged=prepare_and_stage_leased_negative(bounded,artifacts,database,lease,body,source,
        calibration,report,policy,artifacts.scratch,runtime_lock=runtime_lock,runtime_sha=runtime_sha,
        source_max_bytes=source_max_bytes,ram_max_bytes=ram_max_bytes,qualification_authority=qualified,
        verify_color=verify_color,check_active=ctx.check_active)
    budget=lease.execution_profile['resource_estimate']['output_bytes']
    return negative_operation_result(staged,declared_inputs=inputs,output_budget=budget,check_active=checkpoint)
