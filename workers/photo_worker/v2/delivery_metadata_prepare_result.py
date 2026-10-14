"""Two-phase metadata result preparation; no SQL commit or object publication."""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from tempfile import TemporaryDirectory
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataParams,MetadataDocument,MetadataReport
from .adoption import AdoptionInvalid,ArtifactRef
from .delivery_metadata_result import validate_delivery_metadata_result
from .delivery_metadata_files import verify_delivery_metadata_files,VerifiedDeliveryMetadataFiles


@dataclass(frozen=True)
class PreparedDeliveryMetadataResult:
    evidence: VerifiedDeliveryMetadataFiles
    params: ArtifactRef
    root: ArtifactRef
    report: ArtifactRef
    raster: ArtifactRef


def prepare_delivery_metadata_result(reader,params_ref,root,report_ref,task,encoded_ref,raw,pin,
        binding,metadata_ref,source_raw,*,policy,approved_fields=(),scratch_root,max_bytes,max_pixels,
        check_active=lambda:None):
    check_active()
    if (not isinstance(scratch_root,Path) or not scratch_root.is_absolute()
            or scratch_root.resolve(strict=True)!=scratch_root or not scratch_root.is_dir()
            or type(max_bytes) is not int or not 1<=max_bytes<=2**53-1
            or type(max_pixels) is not int or not 1<=max_pixels<=200000000):
        raise AdoptionInvalid('DELIVERY_METADATA_INSTALLATION')
    params_ref=ArtifactRef.parse({'artifact_id':str(params_ref.artifact_id),'sha256':params_ref.sha256})
    root=ArtifactRef.parse({'artifact_id':str(root.artifact_id),'sha256':root.sha256})
    report_ref=ArtifactRef.parse({'artifact_id':str(report_ref.artifact_id),'sha256':report_ref.sha256})
    snapshots={}
    def document(ref,model,source,role=None):
        check_active();e=reader.evidence(ref)
        if (e.ref!=ref or e.source!=source or role is not None and e.role!=role
                or e.media_type!='application/json' or type(e.bytes) is not int or not 2<=e.bytes<=1048576):
            raise AdoptionInvalid('DELIVERY_METADATA_DOCUMENT_EVIDENCE')
        body=reader.read_json(ref,1048576)
        if not isinstance(body,bytes) or len(body)!=e.bytes or sha256(body).hexdigest()!=ref.sha256:
            raise AdoptionInvalid('DELIVERY_METADATA_DOCUMENT_BYTES')
        value=model.model_validate_json(body)
        if canonical_bytes(value)!=body:raise AdoptionInvalid('DELIVERY_METADATA_DOCUMENT_CANONICAL')
        snapshots[ref]=e;check_active();return value,body
    def stream(ref,path,source,media):
        check_active();e=reader.evidence(ref)
        if (e.ref!=ref or e.source!=source or e.role!='encoded_image' or e.media_type!=media
                or type(e.bytes) is not int or not 1<=e.bytes<=max_bytes):
            raise AdoptionInvalid('DELIVERY_METADATA_RASTER_EVIDENCE')
        snapshots[ref]=e;iterator=iter(reader.iter_bytes(ref,e.bytes));count=0;digest=sha256()
        try:
            with path.open('xb') as target:
                for block in iterator:
                    check_active()
                    if not isinstance(block,bytes) or not 1<=len(block)<=65536 or count+len(block)>e.bytes:
                        raise AdoptionInvalid('DELIVERY_METADATA_RASTER_STREAM')
                    target.write(block);digest.update(block);count+=len(block)
        finally:
            close=getattr(iterator,'close',None)
            if close is not None:close()
        if count!=e.bytes or digest.hexdigest()!=ref.sha256:raise AdoptionInvalid('DELIVERY_METADATA_RASTER_BYTES')
    try:
        params,_=document(params_ref,MetadataParams,'input')
        before_ref=ArtifactRef(params.encoded.artifact_id,params.encoded.sha256)
        before,_=document(before_ref,EncodedImage,'input')
        _,projection_raw=document(ArtifactRef(params.document.artifact_id,params.document.sha256),MetadataDocument,'input','v30_metadata_projection')
        after,_=document(root,EncodedImage,'output','encoded_manifest')
        report,_=document(report_ref,MetadataReport,'output','metadata_report')
        original=ArtifactRef(before.artifact.artifact_id,before.artifact.sha256)
        raster=ArtifactRef(after.artifact.artifact_id,after.artifact.sha256)
        refs=(params_ref,before_ref,ArtifactRef(params.document.artifact_id,params.document.sha256),root,report_ref,original,raster)
        if len({ref.artifact_id for ref in refs})!=len(refs):raise AdoptionInvalid('DELIVERY_METADATA_RESULT_ALIAS')
        # Reject invalid authority before downloading raster bytes. The physical
        # verifier repeats this binding after IO.
        logical=validate_delivery_metadata_result(task,encoded_ref,params,before,after,report,
            raw,pin,binding,metadata_ref,source_raw,policy=policy,approved_fields=approved_fields)
        if logical.projection_jcs!=projection_raw:
            raise AdoptionInvalid('DELIVERY_METADATA_PROJECTION_BYTES')
        outputs={root,report_ref,raster}
        if reader.staged_outputs()!=outputs:raise AdoptionInvalid('DELIVERY_METADATA_OUTPUT_CLOSURE')
        with TemporaryDirectory(prefix='delivery-metadata-',dir=scratch_root) as directory:
            before_path,after_path=Path(directory)/'before.bin',Path(directory)/'after.bin'
            media={'jpeg':'image/jpeg','png':'image/png','tiff':'image/tiff'}[before.format]
            stream(original,before_path,'input',media);stream(raster,after_path,'output',media)
            verified=verify_delivery_metadata_files(before_path,after_path,task,encoded_ref,params,before,after,report,
                raw,pin,binding,metadata_ref,source_raw,policy=policy,approved_fields=approved_fields,
                max_bytes=max_bytes,max_pixels=max_pixels,check_active=check_active)
            if verified.documents.projection_jcs!=projection_raw:
                raise AdoptionInvalid('DELIVERY_METADATA_PROJECTION_BYTES')
        for ref,evidence in snapshots.items():
            check_active()
            if reader.evidence(ref)!=evidence:raise AdoptionInvalid('DELIVERY_METADATA_EVIDENCE_CHANGED')
        if reader.staged_outputs()!=outputs:raise AdoptionInvalid('DELIVERY_METADATA_OUTPUT_CLOSURE_CHANGED')
        check_active()
        return PreparedDeliveryMetadataResult(verified,params_ref,root,report_ref,raster)
    except AdoptionInvalid:raise
    except ValueError as error:raise AdoptionInvalid('DELIVERY_METADATA_PHYSICAL_INVALID') from error
