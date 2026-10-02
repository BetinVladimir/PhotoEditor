"""Lens EXIF resolution through compiler-authorized original artifact bindings."""
from typing import Literal
from photo_worker.contracts.base import ArtifactRef,Contract,canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.runtime.executor import OperationFailure
from .exif import inspect_lens_metadata


class OriginalLensBinding(Contract):
    binding:VersionBinding
    original:ArtifactRef
    suffix:Literal['.jpg','.jpeg','.png','.tif','.tiff','.dng','.nef','.cr2','.cr3','.arw','.raf','.rw2','.orf','.pef']


class ArtifactLensMetadataResolver:
    """Bindings must originate from authoritative compiler/catalog resolution.

    Originals must also be declared in the task input closure. Exact VersionBinding
    comparison forbids substituting metadata from another source/canvas/version.
    """
    def __init__(self,artifacts,bindings,check_active=lambda:None):
        self.artifacts=artifacts;self.bindings=tuple(bindings);self.check_active=check_active

    @classmethod
    def from_declared_inputs(cls, artifacts, check_active=lambda:None):
        refs=artifacts.declared_contract_refs('OriginalLensBinding',maximum=1)
        if len(refs)!=1:raise OperationFailure('LENS_METADATA_UNAVAILABLE')
        binding=artifacts.contract(refs[0],OriginalLensBinding)
        source=artifacts.contract(binding.binding.source,WorkingImage)
        if (canonical_hash(source.canvas)!=binding.binding.canvas_hash or
                source.canvas.source_sha256!=binding.original.sha256 or source.dtype!='float32'):
            raise OperationFailure('LENS_METADATA_UNAVAILABLE')
        return cls(artifacts,[binding],check_active)

    def __call__(self,binding):
        selected=[item for item in self.bindings if item.binding==binding]
        if len(selected)!=1:raise OperationFailure('LENS_METADATA_UNAVAILABLE')
        item=selected[0]
        self.check_active()
        with self.artifacts.input(item.original,suffix=item.suffix) as local:
            return inspect_lens_metadata(local.path,binding,self.check_active)
