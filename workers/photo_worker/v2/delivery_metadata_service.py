"""Metadata verification coordinator; export adoption remains a separate step."""
from dataclasses import dataclass
from .adoption import AdoptionInvalid
from .delivery_metadata_authority import SQLMetadataResultAuthority
from .delivery_metadata_reader import DeliveryMetadataEvidenceReader,MetadataResultScope
from .delivery_metadata_prepare_result import prepare_delivery_metadata_result,PreparedDeliveryMetadataResult


@dataclass(frozen=True)
class PreparedCurrentDeliveryMetadata:
    scope: MetadataResultScope
    result: PreparedDeliveryMetadataResult


def prepare_current_delivery_metadata(connection,objects,*,scope,params_ref,root,report_ref,
        encoded_ref,raw,pin,binding,metadata_ref,source_raw,allowed_bucket,scratch_root,
        max_bytes,max_pixels,policy,approved_fields=(),check_active=lambda:None):
    check_active()
    authority=SQLMetadataResultAuthority(connection,scope=scope)
    reader=DeliveryMetadataEvidenceReader(authority,objects,scope=scope,
        allowed_bucket=allowed_bucket,check_active=check_active)
    result=prepare_delivery_metadata_result(reader,params_ref,root,report_ref,scope.task_id,encoded_ref,
        raw,pin,binding,metadata_ref,source_raw,policy=policy,approved_fields=approved_fields,
        scratch_root=scratch_root,max_bytes=max_bytes,max_pixels=max_pixels,check_active=check_active)
    check_active()
    return PreparedCurrentDeliveryMetadata(scope,result)


def _commit_prepared_delivery_metadata(connection,*,scope,prepared,adopt,check_active=lambda:None):
    """Own a short write transaction; no object IO. Cancellation rolls it back."""
    check_active()
    if (connection.autocommit is not True or not isinstance(scope,MetadataResultScope)
            or not isinstance(prepared,PreparedCurrentDeliveryMetadata)
            or prepared.scope!=scope or not isinstance(prepared.result,PreparedDeliveryMetadataResult)):
        raise AdoptionInvalid('DELIVERY_METADATA_VERIFICATION_SCOPE')
    result=prepared.result
    documents=result.evidence.documents
    raster=result.evidence.raster
    from hashlib import sha256
    from photo_worker.contracts.export import EncodedImage
    from photo_worker.contracts.base import canonical_bytes
    manifest=EncodedImage.model_validate_json(documents.after_jcs)
    if (sha256(documents.params_jcs).hexdigest()!=result.params.sha256
            or sha256(documents.after_jcs).hexdigest()!=result.root.sha256
            or sha256(documents.report_jcs).hexdigest()!=result.report.sha256
            or manifest.artifact.artifact_id!=result.raster.artifact_id
            or manifest.artifact.sha256!=result.raster.sha256
            or raster.sha256!=result.raster.sha256
            or canonical_bytes(manifest)!=documents.after_jcs):
        raise AdoptionInvalid('DELIVERY_METADATA_VERIFICATION_RECEIPT')
    with connection.transaction():
        check_active()
        connection.execute("SELECT set_config('app.tenant_id',%s,true)",(str(scope.tenant_id),))
        row=connection.execute(
            ('SELECT photo.adopt_v30_delivery_metadata(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)' if adopt else
             'SELECT photo.record_v30_metadata_verification(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'),
            (scope.tenant_id,scope.task_id,scope.attempt,scope.fence,scope.result_id,
             documents.params_jcs.decode('utf-8'),documents.before_jcs.decode('utf-8'),
             documents.after_jcs.decode('utf-8'),documents.report_jcs.decode('utf-8'),
             documents.projection_jcs.decode('utf-8'),raster.bytes)).fetchone()
        if row is None or len(row)!=1 or row[0] is not True:
            raise AdoptionInvalid('DELIVERY_METADATA_VERIFICATION_REFUSED')
        check_active()
    return True


def record_prepared_delivery_metadata(connection,*,scope,prepared,check_active=lambda:None):
    return _commit_prepared_delivery_metadata(connection,scope=scope,prepared=prepared,
        adopt=False,check_active=check_active)


def adopt_prepared_delivery_metadata(connection,*,scope,prepared,check_active=lambda:None):
    """Atomically adopt metadata export/item; transaction contains no object IO."""
    return _commit_prepared_delivery_metadata(connection,scope=scope,prepared=prepared,
        adopt=True,check_active=check_active)


def adopt_current_delivery_metadata(connection,objects,*,scope,allowed_bucket,scratch_root,
        max_bytes,max_pixels,check_active=lambda:None):
    """Load SQL authority, independently verify physical outputs, then adopt.

    Original metadata/policy are never accepted from a caller-authored receipt.
    Every object read and final commit repeats current result authority.
    """
    from .delivery_metadata_adoption_input import load_metadata_adoption_input,restore_metadata_adoption_proof
    current=load_metadata_adoption_input(connection,scope=scope,check_active=check_active)
    proof=restore_metadata_adoption_proof(current.proof_jcs,current.proof_sha256,scope)
    prepared=prepare_current_delivery_metadata(connection,objects,scope=scope,params_ref=proof.params,
        root=current.root,report_ref=current.report,encoded_ref=proof.encoded,
        raw=proof.frozen_jcs.encode(),pin=proof.frozen_sha256,binding=proof.binding,
        metadata_ref=proof.metadata_source,source_raw=proof.metadata_jcs.encode(),policy=proof.policy,
        approved_fields=tuple(proof.approved_fields),allowed_bucket=allowed_bucket,scratch_root=scratch_root,
        max_bytes=max_bytes,max_pixels=max_pixels,check_active=check_active)
    check_active()
    return adopt_prepared_delivery_metadata(connection,scope=scope,prepared=prepared,check_active=check_active)
