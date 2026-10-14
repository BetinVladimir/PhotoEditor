"""Physical raster qualification of declared prepared portable backgrounds."""
from photo_worker.contracts.base import canonical_bytes, ArtifactRef as WireRef
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid, ArtifactRef


class PreparedImportBackgroundQualifier:
    def __init__(self, *, canonical_images, reader, scratch, check_active=lambda: None):
        self.bindings = {}
        for binding, _ in canonical_images:
            owned = VersionBinding.model_validate_json(canonical_bytes(binding))
            if owned.asset_version_id in self.bindings:
                raise ValueError('INTERCHANGE_BACKGROUND_BINDING')
            self.bindings[owned.asset_version_id] = owned
        if reader is None or scratch is None or not callable(check_active):
            raise ValueError('INTERCHANGE_BACKGROUND_INSTALLATION')
        self.reader, self.scratch, self.active = reader, scratch, check_active

    def __call__(self, reference, contract, binding):
        self.active()
        reference = WireRef.model_validate_json(canonical_bytes(reference))
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        if contract != 'StyleBackgroundAsset' or self.bindings.get(binding.asset_version_id) != binding:
            raise AdoptionInvalid('INTERCHANGE_BACKGROUND_BINDING')
        evidence = self.reader.evidence(ArtifactRef(reference.artifact_id, reference.sha256))
        suffix = {'image/jpeg': '.jpg', 'image/png': '.png', 'image/tiff': '.tiff'}.get(evidence.media_type)
        if (evidence.source != 'input' or evidence.ref != ArtifactRef(reference.artifact_id, reference.sha256)
                or suffix is None or type(evidence.bytes) is not int or not 8 <= evidence.bytes <= 536870912):
            raise AdoptionInvalid('INTERCHANGE_BACKGROUND_EVIDENCE')
        from photo_worker.operations.interchange.export_images import qualify_prepared_background
        qualify_prepared_background(reference.model_copy(deep=True), evidence.media_type,
                                    self.reader, self.scratch, active=self.active)
        self.active()
        return True
