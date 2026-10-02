"""Pinned operator qualification and source-bound lens metadata admission."""
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.runtime.executor import OperationFailure
from photo_worker.algorithms.lens import match_lens_metadata


@dataclass(frozen=True)
class LensMetadataEvidence:
    asset_version_id: object
    source: ArtifactRef
    camera: str
    lens: str
    focal_mm: float
    aperture: float


@dataclass(frozen=True)
class QualifiedLensPin:
    qualification: ArtifactRef
    coefficients_sha: str
    runtime_lock_sha: str
    dataset_sha: str


class PinnedLensAdmission:
    """Pins come from operator-approved registry; metadata resolver is trusted IO.

    The resolver must obtain original EXIF for the exact asset version/source,
    not params supplied by the editor. Qualification artifact SHA is checked via
    declared TaskArtifacts input before its operator pin is accepted. This class
    does not manufacture qualification or infer metadata from profile fields.
    """
    def __init__(self,artifacts,pins,metadata_resolver,runtime_lock_sha,*,runtime_lock):
        self.artifacts=artifacts
        self.pins=tuple(pins)
        self.metadata_resolver=metadata_resolver
        self.runtime_lock_sha=runtime_lock_sha
        self.runtime_lock=runtime_lock

    def admit(self,ctx,params,profile):
        ctx.check_active()
        from .runtime_lock import verify_installed_runtime
        verify_installed_runtime(self.runtime_lock,self.runtime_lock_sha)
        coefficients=profile.model_dump(mode='json');coefficients.pop('qualification')
        digest=canonical_hash(coefficients)
        selected=[pin for pin in self.pins if pin.qualification==profile.qualification and
                  pin.coefficients_sha==digest and pin.runtime_lock_sha==self.runtime_lock_sha]
        if len(selected)!=1:raise OperationFailure('LENS_PROFILE_UNAVAILABLE')
        pin=selected[0]
        if len(pin.dataset_sha)!=64 or any(c not in '0123456789abcdef' for c in pin.dataset_sha):
            raise OperationFailure('CONFIG_INVALID')
        # Materialization enforces task declaration and immutable artifact SHA.
        with self.artifacts.input(pin.qualification,suffix='.json'):
            ctx.check_active()
        metadata=self.metadata_resolver(params.base)
        if (not isinstance(metadata,LensMetadataEvidence) or
                metadata.asset_version_id!=params.base.asset_version_id or metadata.source!=params.base.source):
            raise OperationFailure('LENS_METADATA_UNAVAILABLE')
        match_lens_metadata(profile,metadata.camera,metadata.lens,metadata.focal_mm,metadata.aperture)
        ctx.check_active()
        return metadata.camera,metadata.lens,metadata.focal_mm,metadata.aperture
