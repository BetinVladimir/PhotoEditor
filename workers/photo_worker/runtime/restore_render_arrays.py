"""Route nested render arrays to produced or exact source-reuse authorities."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.operations.session.contracts import VersionBinding
from .restore_render_input_array import verify_fixture_source_array
from .restore_render_array_catalogue import verify_fixture_produced_array
from .restore_authority import require_restore_fixture


def verify_fixture_render_arrays(connection,client,buckets,tenant_id,result,binding,source_raw,
                                  image_raw,changed_raw,generated_raw,*,assert_fixture_scope,check_active):
    if (not isinstance(binding,VersionBinding) or not isinstance(source_raw,bytes)
            or not 2<=len(source_raw)<=1048576 or sha256(source_raw).hexdigest()!=binding.source.sha256):
        raise ValueError('RESTORE_RENDER_ARRAYS_SOURCE')
    source=WorkingImage.model_validate_json(source_raw)
    image=WorkingImage.model_validate_json(image_raw)
    changed=MaskRef.model_validate_json(changed_raw)
    generated=None if generated_raw is None else MaskRef.model_validate_json(generated_raw)
    if (canonical_bytes(source)!=source_raw or canonical_hash(source.canvas)!=binding.canvas_hash
            or image.canvas!=source.canvas or image.transform_chain!=source.transform_chain
            or image.color!=source.color or image.dtype!='float32' or source.dtype!='float32'):
        raise ValueError('RESTORE_RENDER_ARRAYS_GEOMETRY')
    shape=(image.canvas.height,image.canvas.width);evidence=[]
    def guard():require_restore_fixture(check_active,assert_fixture_scope,tenant_id,result)
    guard()
    for ref,original,dimensions,role in ((image.pixels,source.pixels,(*shape,3),'pixels'),
                                         (image.alpha,source.alpha,shape,'alpha')):
        guard()
        if ref is None:continue
        if ref==original:
            item=verify_fixture_source_array(connection,client,buckets,tenant_id,result,binding,source_raw,ref,role,
                assert_fixture_scope=lambda *_:guard(),check_active=guard)
        else:
            item=verify_fixture_produced_array(connection,client,buckets,tenant_id,result,ref,dimensions,role,
                assert_fixture_scope=lambda *_:guard(),check_active=guard)
        evidence.append(item)
    for mask in (changed,generated):
        if mask is None:continue
        guard()
        if mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain:
            raise ValueError('RESTORE_RENDER_ARRAYS_MASK')
        evidence.append(verify_fixture_produced_array(connection,client,buckets,tenant_id,result,mask.artifact,shape,'mask_pixels',
            assert_fixture_scope=lambda *_:guard(),check_active=guard))
    guard();return tuple(evidence)
