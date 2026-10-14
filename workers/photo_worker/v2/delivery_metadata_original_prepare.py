"""Read original through authoritative version-pinned evidence before extraction."""
from hashlib import sha256
from pathlib import Path
from tempfile import TemporaryDirectory
from uuid import UUID
from .adoption import ArtifactRef,AdoptionInvalid
from .delivery_metadata_prepare import prepare_delivery_metadata
from .delivery_metadata_source import MAX_METADATA_ORIGINAL_BYTES
from .delivery_metadata_raw_tool import RAW_METADATA_FILETYPES


def prepare_delivery_metadata_original(reader,original,binding,metadata_id,policy,*,
        approved_fields=(),scratch_root,max_bytes,raw_executable=None,raw_tool_sha=None,
        check_active=lambda:None):
    check_active()
    if (not isinstance(scratch_root,Path) or not scratch_root.is_absolute()
            or scratch_root.resolve(strict=True)!=scratch_root or not scratch_root.is_dir()
            or type(max_bytes) is not int or not 1<=max_bytes<=2**53-1):
        raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_INSTALLATION')
    from photo_worker.contracts.base import ArtifactRef as WireRef,canonical_bytes
    from photo_worker.contracts.metadata import MetadataDocument
    from photo_worker.operations.session.contracts import VersionBinding
    from .delivery_metadata_projection import project_delivery_metadata
    original=WireRef.model_validate_json(canonical_bytes(original))
    binding=VersionBinding.model_validate_json(canonical_bytes(binding))
    if (not isinstance(metadata_id,UUID) or metadata_id.int==0
            or metadata_id in (original.artifact_id,binding.source.artifact_id)
            or original.artifact_id==binding.source.artifact_id):
        raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_BINDING')
    project_delivery_metadata(MetadataDocument(),policy,approved_fields=approved_fields)
    if policy!='approved_exif' and approved_fields:
        raise AdoptionInvalid('DELIVERY_METADATA_APPROVAL')
    ref=ArtifactRef(original.artifact_id,original.sha256)
    evidence=reader.evidence(ref)
    if (evidence.ref!=ref or evidence.source!='input'
            or evidence.media_type not in ('image/jpeg','image/png','image/tiff') and evidence.media_type not in RAW_METADATA_FILETYPES
            or type(evidence.bytes) is not int or not 1<=evidence.bytes<=min(max_bytes,MAX_METADATA_ORIGINAL_BYTES)):
        raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_EVIDENCE')
    try:
        with TemporaryDirectory(prefix='delivery-original-',dir=scratch_root) as directory:
            path=Path(directory)/'original.bin';count=0;digest=sha256()
            iterator=iter(reader.iter_bytes(ref,evidence.bytes))
            try:
                with path.open('xb') as target:
                    for block in iterator:
                        check_active()
                        if not isinstance(block,bytes) or not 1<=len(block)<=65536 or count+len(block)>evidence.bytes:
                            raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_STREAM')
                        target.write(block);digest.update(block);count+=len(block)
            finally:
                close=getattr(iterator,'close',None)
                if close is not None:close()
            if count!=evidence.bytes or digest.hexdigest()!=ref.sha256:
                raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_BYTES')
            prepared=prepare_delivery_metadata(path,original,evidence.media_type,evidence.bytes,binding,
                metadata_id,policy,approved_fields=approved_fields,max_bytes=max_bytes,
                raw_executable=raw_executable,raw_tool_sha=raw_tool_sha,check_active=check_active)
        check_active()
        if reader.evidence(ref)!=evidence:raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_CHANGED')
        check_active();return prepared
    except AdoptionInvalid:raise
    except (ValueError,OSError) as error:raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_INVALID') from error
