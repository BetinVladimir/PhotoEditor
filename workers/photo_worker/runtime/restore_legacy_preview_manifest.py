"""Strict preview manifest semantics with independently pinned source geometry/ICC."""
from dataclasses import dataclass
from hashlib import sha256
import re
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.contracts.media import DerivativeManifest
from .restore_objects import MinioRestoreObjects
from .restore_legacy_preview_result import observe_fixture_legacy_preview_result
from .restore_authority import require_restore_active,require_restore_fixture


@dataclass(frozen=True)
class RestorePreviewManifestEvidence:
    source: ArtifactRef
    derivative: ArtifactRef
    manifest_sha256: str
    width: int
    height: int
    profile_hash: str


def validate_legacy_preview_manifest(raw,target,binding,result,*,source_width,source_height,srgb_sha256,check_active):
    # Geometry and ICC pins must come from the installed fixture's independently
    # verified WorkingImage/ICC registry, never from this manifest or JPEG header.
    from photo_worker.operations.media.derivative import dimensions,media_profile_hash
    if (type(raw) is not bytes or not 2<=len(raw)<=1048576
            or any(type(v) is not int or not 1<=v<=200000 for v in (source_width,source_height))
            or source_width*source_height>200000000
            or not isinstance(srgb_sha256,str) or re.fullmatch('[0-9a-f]{64}',srgb_sha256) is None
            or not callable(check_active)):
        raise ValueError('RESTORE_PREVIEW_MANIFEST_INSTALLATION')
    require_restore_active(check_active);manifest=DerivativeManifest.model_validate_json(raw)
    if canonical_bytes(parse_json(raw))!=raw or canonical_bytes(manifest)!=raw:
        raise ValueError('RESTORE_PREVIEW_MANIFEST_CANONICAL')
    source=ArtifactRef(artifact_id=binding.source_artifact_id,sha256=binding.source_sha256)
    derivative=ArtifactRef(artifact_id=result.derivative.artifact_id,sha256=result.derivative.object.sha256)
    variant='preview1600' if target.scale=='proxy' else 'preview2560'
    width,height=source_width,source_height
    if target.roi is not None:
        x,y,width,height=target.roi
        if x<0 or y<0 or width<1 or height<1 or x+width>source_width or y+height>source_height:
            raise ValueError('RESTORE_PREVIEW_MANIFEST_ROI')
    expected_width,expected_height=dimensions(width,height,variant)
    if (manifest.source!=source or manifest.derivative!=derivative or manifest.variant!=variant
            or manifest.width!=expected_width or manifest.height!=expected_height
            or manifest.profile_hash!=media_profile_hash(variant,srgb_sha256)
            or manifest.media_type!='image/jpeg' or manifest.color_profile!='srgb'
            or manifest.metadata_policy!='stripped' or manifest.alpha):
        raise ValueError('RESTORE_PREVIEW_MANIFEST_BINDING')
    if sha256(raw).hexdigest()!=result.manifest.object.sha256 or len(raw)!=result.manifest.object.size:
        raise ValueError('RESTORE_PREVIEW_MANIFEST_SHA')
    require_restore_active(check_active);return RestorePreviewManifestEvidence(source,derivative,sha256(raw).hexdigest(),
        manifest.width,manifest.height,manifest.profile_hash)


def verify_fixture_legacy_preview_manifest(connection,client,buckets,target,binding,ticket,*,expected_database,
        source_width,source_height,srgb_sha256,assert_fixture_scope,check_active):
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding,ticket)
    def observe():return observe_fixture_legacy_preview_result(connection,target,binding,ticket,
        expected_database=expected_database,assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    before=observe();obj=before.manifest.object
    guard()
    store=MinioRestoreObjects(client,buckets,check_active=guard)
    with store.open_version(obj) as stream:
        chunks=[];size=0
        while True:
            guard();block=stream.read(min(65536,obj.size-size+1))
            if not block:break
            size+=len(block)
            if size>obj.size:raise ValueError('RESTORE_PREVIEW_MANIFEST_SIZE')
            chunks.append(block)
    evidence=validate_legacy_preview_manifest(b''.join(chunks),target,binding,before,
        source_width=source_width,source_height=source_height,srgb_sha256=srgb_sha256,check_active=guard)
    if observe()!=before:raise ValueError('RESTORE_PREVIEW_MANIFEST_CHANGED')
    guard();return evidence
