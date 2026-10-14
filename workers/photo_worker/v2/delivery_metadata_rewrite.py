"""Physical frozen-metadata rewrite; publication remains task/SQL-authorized."""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
import stat
from uuid import UUID
from photo_worker.contracts.export import EncodeParams,EncodedImage
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.metadata.process import run_metadata_tool
from photo_worker.runtime.image_fingerprint import decoded_image_fingerprint
from .delivery_encoded_verify import verify_delivery_encoded_file
from .delivery_metadata_encoded_verify import verify_delivery_metadata_encoded_file
from .delivery_metadata_write import delivery_metadata_write_args


@dataclass(frozen=True)
class RewrittenDeliveryMetadata:
    manifest_jcs: bytes
    bytes: int
    sha256: str
    copied_fields: tuple[str,...]


def rewrite_delivery_metadata(input_path,output_path,executable,encoded_ref,encoded,params,source,
        output_id,raw,pin,binding,metadata_ref,source_raw,*,policy,approved_fields=(),
        max_bytes,max_pixels,check_active=lambda:None,runner=run_metadata_tool):
    check_active()
    if not isinstance(output_id,UUID):raise ValueError('DELIVERY_METADATA_REWRITE_ID')
    # Own contracts before callbacks/tool invocation can mutate caller models.
    encoded_ref=ArtifactRef.model_validate_json(canonical_bytes(encoded_ref))
    metadata_ref=ArtifactRef.model_validate_json(canonical_bytes(metadata_ref))
    encoded=EncodedImage.model_validate_json(canonical_bytes(encoded))
    params=EncodeParams.model_validate_json(canonical_bytes(params))
    source=WorkingImage.model_validate_json(canonical_bytes(source))
    binding=VersionBinding.model_validate_json(canonical_bytes(binding))
    input_path,output_path=Path(input_path),Path(output_path)
    if (not input_path.is_absolute() or input_path.resolve(strict=True)!=input_path
            or not output_path.is_absolute() or output_path.parent.resolve(strict=True)!=output_path.parent
            or output_path.exists() or output_path.is_symlink() or input_path==output_path
            or output_id.int==0 or output_id in (encoded.artifact.artifact_id,encoded_ref.artifact_id,
                binding.source.artifact_id,metadata_ref.artifact_id)):
        raise ValueError('DELIVERY_METADATA_REWRITE_PATH')
    info=input_path.stat(follow_symlinks=False)
    if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1:
        raise ValueError('DELIVERY_METADATA_REWRITE_SOURCE')
    args,copied=delivery_metadata_write_args(executable,output_path,encoded_ref,encoded,raw,pin,
        binding,metadata_ref,source_raw,policy=policy,approved_fields=approved_fields,check_active=check_active)
    verify_delivery_encoded_file(input_path,params,source,encoded,max_bytes=max_bytes,
        max_pixels=max_pixels,check_active=check_active)
    mime={'jpeg':'image/jpeg','png':'image/png','tiff':'image/tiff'}[encoded.format]
    created=False
    try:
        with output_path.open('xb') as target:
            created=True;count=0;digest=sha256()
            with input_path.open('rb') as original:
                while block:=original.read(65536):
                    check_active();count+=len(block)
                    if count>max_bytes:raise ValueError('DELIVERY_METADATA_REWRITE_SIZE')
                    target.write(block);digest.update(block)
        if count!=info.st_size or digest.hexdigest()!=encoded.artifact.sha256:
            raise ValueError('DELIVERY_METADATA_REWRITE_SOURCE_CHANGED')
        with output_path.open('rb') as original:
            pixels=decoded_image_fingerprint(original,mime,preserve_orientation=False,check_active=check_active)
        check_active()
        completed=runner(args,check_active)
        check_active()
        if completed.returncode!=0:raise ValueError('DELIVERY_METADATA_REWRITE_TOOL')
        after=output_path.stat(follow_symlinks=False)
        if not stat.S_ISREG(after.st_mode) or after.st_nlink!=1 or not 1<=after.st_size<=max_bytes:
            raise ValueError('DELIVERY_METADATA_REWRITE_SIZE')
        count=0;digest=sha256()
        with output_path.open('rb') as rewritten:
            while block:=rewritten.read(65536):
                check_active();count+=len(block)
                if count>max_bytes:raise ValueError('DELIVERY_METADATA_REWRITE_SIZE')
                digest.update(block)
        manifest=encoded.model_copy(update={'artifact':ArtifactRef(artifact_id=output_id,sha256=digest.hexdigest()),
            'metadata_policy':'copyright_only' if policy=='copyright' else 'safe_camera'})
        verified=verify_delivery_metadata_encoded_file(output_path,params,source,manifest,raw,pin,
            binding,metadata_ref,source_raw,policy=policy,approved_fields=approved_fields,max_bytes=max_bytes,
            max_pixels=max_pixels,check_active=check_active)
        with output_path.open('rb') as rewritten:
            if decoded_image_fingerprint(rewritten,mime,preserve_orientation=False,check_active=check_active)!=pixels:
                raise ValueError('DELIVERY_METADATA_REWRITE_PIXELS')
        check_active()
        identity=lambda item:(item.st_dev,item.st_ino,item.st_size,item.st_mtime_ns,item.st_ctime_ns,item.st_mode,item.st_nlink)
        if identity(after)!=identity(output_path.stat(follow_symlinks=False)):
            raise ValueError('DELIVERY_METADATA_REWRITE_CHANGED')
        return RewrittenDeliveryMetadata(canonical_bytes(manifest),verified.bytes,verified.sha256,copied)
    except BaseException:
        if created:output_path.unlink(missing_ok=True)
        raise
