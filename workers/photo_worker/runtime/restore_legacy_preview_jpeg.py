"""Physical preview JPEG verification; no perceptual-quality claim."""
from dataclasses import dataclass
from hashlib import sha256
from io import BytesIO
from PIL import Image
from .restore_inventory import RestoreObject,_validate
from .restore_objects import MinioRestoreObjects
from .restore_legacy_preview_manifest import RestorePreviewManifestEvidence
from .restore_legacy_preview_result import observe_fixture_legacy_preview_result
from .restore_authority import require_restore_active,require_restore_fixture


@dataclass(frozen=True)
class RestorePreviewJPEGEvidence:
    sha256: str
    bytes_verified: int
    width: int
    height: int
    icc_sha256: str


def validate_legacy_preview_jpeg(raw,obj,manifest,*,srgb_profile,check_active):
    if (type(raw) is not bytes or not 1<=len(raw)<=67108864 or not isinstance(obj,RestoreObject)
            or not isinstance(manifest,RestorePreviewManifestEvidence)
            or type(srgb_profile) is not bytes or not 128<=len(srgb_profile)<=4194304
            or not callable(check_active)
            or any(type(v) is not int or not 1<=v<=2560 for v in (manifest.width,manifest.height))):
        raise ValueError('RESTORE_PREVIEW_JPEG_INSTALLATION')
    _validate(obj);require_restore_active(check_active)
    digest=sha256(raw).hexdigest()
    if len(raw)!=obj.size or digest!=obj.sha256 or digest!=manifest.derivative.sha256:
        raise ValueError('RESTORE_PREVIEW_JPEG_SHA')
    # Require JPEG boundary markers; immutable hash binds the complete stream.
    # This is not a general JPEG polyglot/security scanner.
    if not raw.startswith(b'\xff\xd8') or not raw.endswith(b'\xff\xd9'):
        raise ValueError('RESTORE_PREVIEW_JPEG_ENCODING')
    with Image.open(BytesIO(raw)) as image:
        if (image.format!='JPEG' or image.mode!='RGB' or image.size!=(manifest.width,manifest.height)
                or image.info.get('icc_profile')!=srgb_profile
                or image.getexif() or any(k in image.info for k in ('exif','xmp','XML:com.adobe.xmp','comment'))):
            raise ValueError('RESTORE_PREVIEW_JPEG_SEMANTICS')
        require_restore_active(check_active);image.verify()
    with Image.open(BytesIO(raw)) as image:
        require_restore_active(check_active);image.load();require_restore_active(check_active)
    return RestorePreviewJPEGEvidence(digest,len(raw),manifest.width,manifest.height,sha256(srgb_profile).hexdigest())


def verify_fixture_legacy_preview_jpeg(connection,client,buckets,target,binding,ticket,manifest,*,expected_database,
                                      srgb_profile,assert_fixture_scope,check_active):
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding,ticket)
    def observe():return observe_fixture_legacy_preview_result(connection,target,binding,ticket,
        expected_database=expected_database,assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    from photo_worker.operations.media.derivative import media_profile_hash
    if (type(srgb_profile) is not bytes or not 128<=len(srgb_profile)<=4194304
            or manifest.profile_hash!=media_profile_hash('preview1600' if target.scale=='proxy' else 'preview2560',
                sha256(srgb_profile).hexdigest())):
        raise ValueError('RESTORE_PREVIEW_JPEG_PROFILE')
    before=observe()
    if before.derivative.artifact_id!=manifest.derivative.artifact_id:
        raise ValueError('RESTORE_PREVIEW_JPEG_REFERENCE')
    obj=before.derivative.object
    guard()
    store=MinioRestoreObjects(client,buckets,check_active=guard)
    with store.open_version(obj) as stream:
        chunks=[];size=0
        while True:
            guard();block=stream.read(min(65536,obj.size-size+1))
            if not block:break
            size+=len(block)
            if size>obj.size or size>67108864:raise ValueError('RESTORE_PREVIEW_JPEG_SIZE')
            chunks.append(block)
    evidence=validate_legacy_preview_jpeg(b''.join(chunks),obj,manifest,srgb_profile=srgb_profile,check_active=guard)
    if observe()!=before:raise ValueError('RESTORE_PREVIEW_JPEG_CHANGED')
    guard();return evidence
