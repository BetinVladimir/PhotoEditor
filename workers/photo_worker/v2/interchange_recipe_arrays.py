"""Physical float32 qualification of declared canonical recipe arrays."""
from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.defects import DefectCandidates
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.algorithms.defect_labels import validate_labels
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.runtime.arrays import MAX_HEADER, read_array, read_inverse_field, read_lut
from .adoption import ArtifactRef, AdoptionInvalid


class PreparedImportArrayQualifier:
    # Explicit transport vocabulary only. The canonical-image/mask witness
    # still determines shape, dtype, range and source; labels grant no authority.
    TRANSPORT_ROLES = {
        'WorkingPixels': 'WorkingPixels', 'PixelArray': 'WorkingPixels',
        'WorkingAlpha': 'WorkingAlpha', 'DefectLabelMap': 'DefectLabelMap',
        'AbsoluteLUTArray': 'AbsoluteLUTArray',
        'MaskArray': 'MaskArray', 'MaskPixels': 'MaskArray',
        'DenseWarpArray': 'DenseWarpArray', 'DenseWarpField': 'DenseWarpArray',
    }

    def __init__(self, *, canonical_images, reader, scratch, max_file_bytes,
                 check_active=lambda: None):
        if (type(max_file_bytes) is not int or not 1 <= max_file_bytes <= 128 << 30
                or reader is None or scratch is None or not callable(check_active)):
            raise ValueError('INTERCHANGE_ARRAY_INSTALLATION')
        self.reader, self.scratch, self.active = reader, scratch, check_active
        self.maximum = max_file_bytes
        self.images, self.specs, self.canonical_arrays = {}, {}, {}
        self.defect_proposals = {}
        self.defect_proposal_count = 0
        for binding, image in canonical_images:
            binding = VersionBinding.model_validate_json(canonical_bytes(binding))
            image = WorkingImage.model_validate_json(canonical_bytes(image))
            if (binding.asset_version_id.int == 0 or binding.source.artifact_id.int == 0
                    or canonical_hash(image.canvas) != binding.canvas_hash
                    or sha256(canonical_bytes(image)).hexdigest() != binding.source.sha256
                    or image.dtype != 'float32' or image.canvas.width * image.canvas.height > 200000000
                    or binding.asset_version_id in self.images):
                raise ValueError('INTERCHANGE_ARRAY_CANONICAL_IMAGE')
            self.images[binding.asset_version_id] = (binding, image)
            h, w = image.canvas.height, image.canvas.width
            self._remember(binding, image.pixels, 'WorkingPixels', (h, w, 3), False, canonical=True)
            if image.alpha is not None:
                self._remember(binding, image.alpha, 'WorkingAlpha', (h, w), True, canonical=True)
            # Dense inverse fields live on each step's output grid, which can
            # differ from the final image canvas after later crop/resize steps.
            for step in image.transform_chain.steps:
                if step.kind == 'dense_warp':
                    if (step.from_canvas.width * step.from_canvas.height > 200000000
                            or step.to_canvas.width * step.to_canvas.height > 200000000):
                        raise ValueError('INTERCHANGE_ARRAY_WARP_CANVAS')
                    self._remember(binding, step.inverse_field, 'DenseWarpArray',
                        (step.to_canvas.height, step.to_canvas.width, 2), False, canonical=True)
            if len(self.images) > 5000:
                raise ValueError('INTERCHANGE_ARRAY_LIMIT')

    def _remember(self, binding, reference, contract, shape, unit_interval, *, canonical=False):
        key = (binding.asset_version_id, reference.artifact_id)
        spec = (reference.sha256, contract, shape, unit_interval)
        if reference.artifact_id.int == 0 or key in self.specs and self.specs[key] != spec:
            raise AdoptionInvalid('INTERCHANGE_ARRAY_CONFLICT')
        existing = self.canonical_arrays.get(reference.artifact_id)
        if existing is not None and existing != spec:
            raise AdoptionInvalid('INTERCHANGE_ARRAY_CONFLICT')
        if key not in self.specs and len(self.specs) >= 30000:
            raise AdoptionInvalid('INTERCHANGE_ARRAY_LIMIT')
        if canonical:
            self.canonical_arrays[reference.artifact_id] = spec
        self.specs[key] = spec

    def register_mask(self, binding, mask):
        """Called only after the parent MaskRef passes semantic/runtime checks."""
        self.active()
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        mask = MaskRef.model_validate_json(canonical_bytes(mask))
        owned = self.images.get(binding.asset_version_id)
        if (owned is None or owned[0] != binding or mask.canvas != owned[1].canvas
                or mask.transform_chain != owned[1].transform_chain):
            raise AdoptionInvalid('INTERCHANGE_ARRAY_MASK_GEOMETRY')
        self._remember(binding, mask.artifact, 'MaskArray', (mask.canvas.height, mask.canvas.width), True)

    def register_lut(self, binding, resolved):
        """Only after complete resolved schema and current runtime authority."""
        self.active()
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        resolved = ResolvedStyle.model_validate_json(canonical_bytes(resolved))
        owned = self.images.get(binding.asset_version_id)
        if owned is None or owned[0] != binding or resolved.params.image != binding.source or resolved.absolute_lut is None:
            raise AdoptionInvalid('INTERCHANGE_ARRAY_LUT_SOURCE')
        lut = resolved.absolute_lut
        self._remember(binding, lut.artifact, 'AbsoluteLUTArray', (lut.size, lut.size, lut.size, 3), True)
        self.active()

    def register_defect_proposal(self, binding, proposal):
        """Register only after exact proposal semantics and runtime authority."""
        self.active()
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        proposal = DefectCandidates.model_validate_json(canonical_bytes(proposal))
        owned = self.images.get(binding.asset_version_id)
        if (owned is None or owned[0] != binding or proposal.image != binding.source
                or proposal.source_pixels != owned[1].pixels or proposal.canvas != owned[1].canvas
                or proposal.transform_chain != owned[1].transform_chain):
            raise AdoptionInvalid('INTERCHANGE_ARRAY_DEFECT_SOURCE')
        key = (binding.asset_version_id, proposal.label_map.artifact_id)
        # Check the global bound before retaining either the witness or its
        # array spec. Repeated occurrences of identical owned bytes cost no
        # additional slot; distinct proposals sharing labels each cost one.
        digest = canonical_hash(proposal)
        bucket = self.defect_proposals.get(key, {})
        additional = digest not in bucket
        if additional and self.defect_proposal_count >= 30000:
            raise AdoptionInvalid('INTERCHANGE_ARRAY_LIMIT')
        self._remember(binding, proposal.label_map, 'DefectLabelMap',
            (proposal.canvas.height, proposal.canvas.width), False)
        # Every referring inventory must agree with physical labels.
        if additional:
            self.defect_proposals.setdefault(key, {})[digest] = proposal
            self.defect_proposal_count += 1
        self.active()

    def __call__(self, reference, contract, binding):
        reference = reference.model_copy(deep=True)
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        self.active()
        contract = self.TRANSPORT_ROLES.get(contract)
        owned = self.images.get(binding.asset_version_id)
        spec = self.specs.get((binding.asset_version_id, reference.artifact_id))
        # A shared CorrectionPlan can reference other verified canonical images.
        # Their pixels retain their own witnessed shape, never the target shape.
        # Canonical warp fields retain their occurrence grid as well. Masks
        # remain target-bound and cannot use this canonical image fallback.
        if spec is None and contract in ('WorkingPixels', 'WorkingAlpha', 'DenseWarpArray', 'MaskArray'):
            spec = self.canonical_arrays.get(reference.artifact_id)
        # The normative image schema calls alpha MaskArray. Accept it only
        # when the canonical WorkingImage independently witnessed alpha's role.
        if contract == 'MaskArray' and spec is not None and spec[1] == 'WorkingAlpha':
            contract = 'WorkingAlpha'
        if owned is None or owned[0] != binding or spec is None or spec[0] != reference.sha256 or spec[1] != contract:
            raise AdoptionInvalid('INTERCHANGE_ARRAY_UNWITNESSED')
        ref = ArtifactRef(reference.artifact_id, reference.sha256)
        item = self.reader.evidence(ref)
        payload = 2 if contract == 'DefectLabelMap' else 4
        for dimension in spec[2]:
            payload *= dimension
        if (item.ref != ref or item.source != 'input' or item.media_type not in ('application/octet-stream', 'application/x-npy')
                or type(item.bytes) is not int or not 1 <= item.bytes <= min(self.maximum, payload + MAX_HEADER + 12)):
            raise AdoptionInvalid('INTERCHANGE_ARRAY_EVIDENCE')
        with self.scratch.reserve(item.bytes) as reservation:
            path = reservation.path('prepared-import-array', '.npy')
            count, checksum = 0, sha256()
            iterator = iter(self.reader.iter_bytes(ref, item.bytes))
            try:
                with self.scratch.open_writer(path, reservation) as writer:
                    for block in iterator:
                        self.active()
                        if not isinstance(block, bytes) or not 1 <= len(block) <= 1 << 20 or count + len(block) > item.bytes:
                            raise AdoptionInvalid('INTERCHANGE_ARRAY_STREAM')
                        if writer.write(block) != len(block):
                            raise AdoptionInvalid('INTERCHANGE_ARRAY_SHORT_WRITE')
                        checksum.update(block)
                        count += len(block)
            finally:
                close = getattr(iterator, 'close', None)
                if close is not None:
                    close()
            self.active()
            if count != item.bytes or checksum.hexdigest() != ref.sha256:
                raise AdoptionInvalid('INTERCHANGE_ARRAY_HASH')
            reservation.validate_file(path)
            mapped = (read_lut(path, spec[2][0], self.active) if contract == 'AbsoluteLUTArray' else
                      read_inverse_field(path, spec[2][:2], self.active)
                      if contract == 'DenseWarpArray' else
                      read_array(path, spec[2], 'uint16' if contract == 'DefectLabelMap' else 'float32', self.active, unit_interval=spec[3]))
            with mapped as values:
                self.active()
                if contract == 'DefectLabelMap':
                    for proposal in self.defect_proposals.get((binding.asset_version_id, reference.artifact_id), {}).values():
                        validate_labels(values, proposal.model_copy(deep=True), check_active=self.active)
        self.active()
        # Evidence is a fresh adapter observation, not a durable authorization
        # grant. Recheck metadata after scratch IO and physical verification.
        if self.reader.evidence(ref) != item:
            raise AdoptionInvalid('INTERCHANGE_ARRAY_EVIDENCE_CHANGED')
        self.active()
        return True
