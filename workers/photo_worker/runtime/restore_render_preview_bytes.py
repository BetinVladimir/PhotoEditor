"""Physical review-preview bytes bound to independently verified render source."""
from dataclasses import dataclass
from hashlib import sha256
from io import BytesIO
from uuid import UUID
from PIL import Image
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.contracts.media import DerivativeManifest
from photo_worker.operations.media.derivative import dimensions,media_profile_hash
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreRenderPreviewBytes:
    review_id: UUID
    side: str
    source: ArtifactRef
    manifest: ArtifactRef
    image: ArtifactRef
    width: int
    height: int
    media_type: str
    profile_hash: str


def verify_render_preview_bytes(api_raw,manifest_raw,image_raw,*,review_id,side,
                                source,source_width,source_height,source_alpha,
                                variant,srgb_profile,check_active):
    """Validate actual downloaded bytes; URL/version authority is separate."""
    if (not isinstance(review_id,UUID) or review_id.int==0
            or side not in ('before','after') or variant not in ('preview1600','preview2560')
            or not isinstance(source,ArtifactRef) or source.artifact_id.int==0
            or type(source_width) is not int or type(source_height) is not int
            or not 1<=source_width<=200000 or not 1<=source_height<=200000
            or source_width*source_height>200000000 or type(source_alpha) is not bool
            or type(srgb_profile) is not bytes or not 128<=len(srgb_profile)<=4194304
            or any(type(raw) is not bytes for raw in (api_raw,manifest_raw,image_raw))
            or not 2<=len(api_raw)<=65536 or not 2<=len(manifest_raw)<=1048576
            or not 2<=len(image_raw)<=52*1048576 or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_PREVIEW_INSTALLATION')
    require_restore_active(check_active);value=parse_json(api_raw,max_bytes=65536)
    if (not isinstance(value,dict) or set(value)!={'review_id','side','source','profile_hash',
            'width','height','manifest','image'}):
        raise ValueError('RESTORE_RENDER_PREVIEW_API')
    expected_width,expected_height=dimensions(source_width,source_height,variant)
    profile=media_profile_hash(variant,sha256(srgb_profile).hexdigest())
    if (value['review_id']!=str(review_id) or value['side']!=side
            or value['source']!={'artifact_id':str(source.artifact_id),'sha256':source.sha256}
            or value['profile_hash']!=profile
            or type(value['width']) is not int or type(value['height']) is not int
            or (value['width'],value['height'])!=(expected_width,expected_height)):
        raise ValueError('RESTORE_RENDER_PREVIEW_SCOPE')
    refs=[]
    for name,maximum,media in (('manifest',1048576,'application/json'),
                               ('image',52*1048576,'image/webp' if source_alpha else 'image/jpeg')):
        item=value[name]
        if (not isinstance(item,dict) or set(item)!={'artifact','bytes','media_type','url','expires_at'}
                or type(item['bytes']) is not int or not 2<=item['bytes']<=maximum
                or item['media_type']!=media or not isinstance(item['artifact'],dict)
                or set(item['artifact'])!={'artifact_id','sha256'}
                or not isinstance(item['url'],str) or not item['url']
                or not isinstance(item['expires_at'],str)):
            raise ValueError('RESTORE_RENDER_PREVIEW_LINK')
        try:ref=ArtifactRef.model_validate_json(canonical_bytes(item['artifact']))
        except Exception:raise ValueError('RESTORE_RENDER_PREVIEW_LINK') from None
        raw=manifest_raw if name=='manifest' else image_raw
        if len(raw)!=item['bytes'] or sha256(raw).hexdigest()!=ref.sha256:
            raise ValueError('RESTORE_RENDER_PREVIEW_SHA')
        refs.append(ref)
    manifest_ref,image_ref=refs
    if len({source.artifact_id,manifest_ref.artifact_id,image_ref.artifact_id})!=3:
        raise ValueError('RESTORE_RENDER_PREVIEW_ALIAS')
    manifest=DerivativeManifest.model_validate_json(manifest_raw)
    if (canonical_bytes(parse_json(manifest_raw))!=manifest_raw
            or canonical_bytes(manifest)!=manifest_raw
            or manifest.source!=source or manifest.derivative!=image_ref
            or manifest.variant!=variant or manifest.profile_hash!=profile
            or manifest.width!=expected_width or manifest.height!=expected_height
            or manifest.media_type!=value['image']['media_type']
            or manifest.color_profile!='srgb' or manifest.metadata_policy!='stripped'
            or manifest.alpha is not source_alpha):
        raise ValueError('RESTORE_RENDER_PREVIEW_MANIFEST')
    require_restore_active(check_active)
    expected_format='WEBP' if source_alpha else 'JPEG'
    with Image.open(BytesIO(image_raw)) as image:
        if (image.format!=expected_format or image.size!=(expected_width,expected_height)
                or image.mode!=('RGBA' if source_alpha else 'RGB')
                or image.info.get('icc_profile')!=srgb_profile
                or image.getexif() or any(k in image.info for k in
                    ('exif','xmp','XML:com.adobe.xmp','comment'))):
            raise ValueError('RESTORE_RENDER_PREVIEW_IMAGE')
        require_restore_active(check_active);image.verify()
    with Image.open(BytesIO(image_raw)) as image:
        require_restore_active(check_active);image.load();require_restore_active(check_active)
    return RestoreRenderPreviewBytes(review_id,side,source,manifest_ref,image_ref,
        expected_width,expected_height,value['image']['media_type'],profile)
