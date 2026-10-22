"""Explicit installation combining result-scoped semantic adoption authorities."""
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import bind_semantic_transfer_documents
from photo_worker.operations.semantic.color_authority import semantic_working_color_authority
from .adoption import AdoptionInvalid
from .adoption_store import PostgresObjectEvidenceReader
from .semantic_adoption_qualification import semantic_adoption_qualification_loader
from .semantic_catalog_authority import load_semantic_adoption_catalog


def sql_semantic_adoption_authority(connection,objects,bucket,*,working_icc_sha,verify_scene,check_active=lambda:None):
    """Scene verifier must prove pinned planar-background/occlusion evidence.

    No inferred scene proof or fallback is installed. verify_scene receives owned
    params,intent,calibration,images,selected,guards and must raise on failure.
    """
    if (not connection.autocommit or not isinstance(bucket,str) or not bucket
            or not callable(verify_scene) or not callable(check_active)):
        raise ValueError('SEMANTIC_ADOPTION_AUTHORITY_CONFIGURATION_INVALID')
    color=semantic_working_color_authority(working_icc_sha)
    def verify(tenant_id,task_id,context,params,intent,calibration,selected,guards):
        check_active()
        context=deepcopy(context)
        params,intent,calibration=bind_semantic_transfer_documents(params,intent,calibration)
        selected,guards=deepcopy((selected,guards))
        reader=PostgresObjectEvidenceReader(connection,objects,tenant_id=tenant_id,task_id=task_id,
            attempt=context.attempt,fence=context.fence,allowed_bucket=bucket)
        report=semantic_adoption_qualification_loader(connection,tenant_id,task_id,context,params,objects,bucket,check_active=check_active)
        report()
        def catalog():
            return load_semantic_adoption_catalog(connection,tenant_id,task_id,context,reader,params,check_active=check_active)
        images,masks=catalog()
        for image in images:check_active();color(image);check_active()
        if selected is not None and tuple(canonical_bytes(value) for value in guards)!=tuple(canonical_bytes(value) for value in masks):
            raise AdoptionInvalid('SEMANTIC_PROTECTION_INPUTS_CHANGED')
        if selected is None and guards!=[]:
            raise AdoptionInvalid('SEMANTIC_PROTECTION_INPUTS_CHANGED')
        scoped_scene=verify_scene
        bind_scene=getattr(verify_scene,'for_result',None)
        if callable(bind_scene):
            scoped_scene=bind_scene(connection,reader,tenant_id,task_id,context,check_active)
        outcome=scoped_scene(*deepcopy((params,intent,calibration,images,selected,masks)))
        check_active();report()
        after=catalog()
        if tuple(tuple(canonical_bytes(value) for value in group) for group in after)!=tuple(tuple(canonical_bytes(value) for value in group) for group in (images,masks)):
            raise AdoptionInvalid('SEMANTIC_CATALOG_BINDING_CHANGED')
        check_active()
        return outcome
    return verify
