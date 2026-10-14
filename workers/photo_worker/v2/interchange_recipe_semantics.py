"""Concrete source/geometry checks for prepared imported recipe documents."""
from copy import deepcopy
from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.images import WorkingImage, MaskRef, Stats20
from photo_worker.contracts.override import OverrideV1
from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.restoration import DenoiseParams, UpscaleParams
from photo_worker.contracts.portrait_detail import PortraitParams
from photo_worker.contracts.dust import DustRepairParams, DustCandidates
from photo_worker.contracts.defects import DefectRepairParams, DefectCandidates, DefectReview
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.statistics import StatsParams
from photo_worker.operations.session.contracts import VersionBinding, CorrectionPlan
from .adoption import AdoptionInvalid
from .render_layers import RenderLayerStack
from .render_overrides import validate_render_override_source
from .render_recipe import EditingRecipeV3


class PreparedRecipeDocumentQualifier:
    """Own verified canonical images and the installed work ICC pin.

    Runtime qualification is mandatory for every document, including disabled
    layers. It proves model/operator support; source geometry is checked here.
    Extra validators enable installed operator Params without weakening builtins.
    """
    MODELS = {'WorkingImage': WorkingImage, 'MaskRef': MaskRef, 'Override': OverrideV1,
              'StyleV2': StyleV2, 'CorrectionPlan': CorrectionPlan,
              'LayerStack': RenderLayerStack, 'EditingRecipeV3': EditingRecipeV3,
              'ResolvedStyle': ResolvedStyle, 'DustCandidates': DustCandidates,
              'DefectCandidates': DefectCandidates, 'DefectReview': DefectReview, 'Stats20': Stats20}

    def __init__(self, *, canonical_images, work_icc_sha, qualify_runtime,
                 extra_validators=None, array_qualifier=None, check_active=lambda: None,
                 qualify_confirmation=None):
        if (not isinstance(work_icc_sha, str) or len(work_icc_sha) != 64
                or any(c not in '0123456789abcdef' for c in work_icc_sha)
                or not callable(qualify_runtime) or not callable(check_active)):
            raise ValueError('INTERCHANGE_SEMANTICS_INSTALLATION')
        self.images = {}
        self.image_hashes = set()
        for binding, image in canonical_images:
            binding = VersionBinding.model_validate_json(canonical_bytes(binding))
            image = WorkingImage.model_validate_json(canonical_bytes(image))
            if (binding.asset_version_id.int == 0 or binding.source.artifact_id.int == 0
                    or canonical_hash(image.canvas) != binding.canvas_hash
                    or sha256(canonical_bytes(image)).hexdigest() != binding.source.sha256
                    or image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
                    or image.color.encoding != 'gamma24' or image.color.icc_sha256 != work_icc_sha
                    or binding.asset_version_id in self.images):
                raise ValueError('INTERCHANGE_SEMANTICS_CANONICAL_IMAGE')
            self.images[binding.asset_version_id] = (binding, image)
            self.image_hashes.add(binding.source.sha256)
            if len(self.images) > 5000:
                raise ValueError('INTERCHANGE_SEMANTICS_LIMIT')
        self.extra = dict(extra_validators or {})
        if self.extra.keys() & self.MODELS.keys() or any(not callable(v) for v in self.extra.values()):
            raise ValueError('INTERCHANGE_SEMANTICS_VALIDATORS')
        self.runtime, self.active = qualify_runtime, check_active
        if qualify_confirmation is not None and not callable(qualify_confirmation):
            raise ValueError('INTERCHANGE_SEMANTICS_CONFIRMATION_INSTALLATION')
        self.confirmation = qualify_confirmation
        if array_qualifier is not None and not callable(getattr(array_qualifier, 'register_mask', None)):
            raise ValueError('INTERCHANGE_SEMANTICS_ARRAY_INSTALLATION')
        self.arrays = array_qualifier

    PARAM_MODELS = {model.__name__: model for model in
        (PortraitHairParams, PortraitClothingParams, DenoiseParams, UpscaleParams, StatsParams, PortraitParams,
         DustRepairParams, DefectRepairParams)}

    def qualify_params(self, schema_name, value, binding):
        return self('Params', value, binding, params_schema=schema_name)

    def __call__(self, contract, value, binding, *, params_schema=None):
        self.active()
        binding = VersionBinding.model_validate_json(canonical_bytes(binding))
        owned = self.images.get(binding.asset_version_id)
        if owned is None or owned[0] != binding:
            raise AdoptionInvalid('INTERCHANGE_SEMANTICS_BINDING')
        source = owned[1]
        # Freeze caller bytes before any operator callback can mutate them.
        frozen = canonical_bytes(value)
        model = self.MODELS.get(contract)
        if contract == 'Params' and params_schema is not None:
            model = self.PARAM_MODELS.get(params_schema)
        if contract == 'Params' and params_schema is None and isinstance(value, dict) and ('detail' in value or 'wrinkle_strength' in value):
            if ('detail' in value) == ('wrinkle_strength' in value):
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_PARAMS_TYPE')
            model = PortraitHairParams if 'detail' in value else PortraitClothingParams
        if model is None:
            validator = self.extra.get(contract)
            if validator is None:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_CONTRACT')
            typed = validator(parse_json(frozen, max_bytes=8 << 20))
        else:
            typed = model.model_validate_json(frozen)
        if canonical_bytes(typed) != frozen:
            raise AdoptionInvalid('INTERCHANGE_SEMANTICS_FIELDS')
        if contract == 'WorkingImage':
            # Correction plans may reference other canonical images in this import.
            if sha256(canonical_bytes(typed)).hexdigest() not in self.image_hashes:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_WORKING_IMAGE')
        elif contract == 'EditingRecipeV3':
            if typed.source != binding or typed.learning_version_id is not None:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_RECIPE')
        elif contract == 'MaskRef':
            if typed.canvas != source.canvas or typed.transform_chain != source.transform_chain:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_MASK_GEOMETRY')
        elif contract == 'Params' and isinstance(typed, (PortraitHairParams, PortraitClothingParams)):
            if typed.base != binding:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_PARAMS_SOURCE')
        elif contract == 'Params' and isinstance(typed, (DenoiseParams, UpscaleParams, StatsParams, PortraitParams, DustRepairParams, DefectRepairParams)):
            if typed.image != binding.source:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_PARAMS_SOURCE')
            if isinstance(typed, UpscaleParams) and source.canvas.width * source.canvas.height * typed.scale ** 2 > 200_000_000:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_UPSCALE_PIXEL_LIMIT')
            if isinstance(typed, DustRepairParams) and typed.review is not None:
                if typed.review.base != binding.source or typed.review.proposal != typed.proposal:
                    raise AdoptionInvalid('INTERCHANGE_SEMANTICS_DUST_REVIEW_SOURCE')
            masks = []
            if isinstance(typed, PortraitParams):
                masks = [region.mask for region in typed.regions] + list(typed.protected)
            elif isinstance(typed, StatsParams):
                masks = [typed.person]
            elif isinstance(typed, DefectRepairParams):
                masks = [typed.protected]
            for mask in masks:
                if mask.canvas != source.canvas or mask.transform_chain != source.transform_chain:
                    raise AdoptionInvalid('INTERCHANGE_SEMANTICS_MASK_GEOMETRY')
        elif contract == 'Stats20':
            if typed.source_image != binding.source:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_STATS_SOURCE')
        elif contract == 'ResolvedStyle':
            if typed.params.image != binding.source:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_RESOLVED_SOURCE')
            if typed.params.override is not None:
                validate_render_override_source(typed.params.override, source)
                self._qualify_confirmed_masks(typed.params.override, binding)
        elif contract == 'DefectCandidates':
            if (typed.image != binding.source or typed.source_pixels != source.pixels
                    or typed.canvas != source.canvas or typed.transform_chain != source.transform_chain):
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_DEFECT_SOURCE')
        elif contract == 'DefectReview':
            if typed.image != binding.source:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_DEFECT_REVIEW_SOURCE')
        elif contract == 'Override':
            validate_render_override_source(typed, source)
            # Imported candidate identities and confirmations belong to the
            # exporting review context. Source SHA equality cannot authorize
            # their use in this tenant/project or replace a fresh decision.
            if typed.accepted_candidate_ids or typed.rejected_candidate_ids:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_FOREIGN_REVIEW')
            self._qualify_confirmed_masks(typed, binding)
        elif contract == 'LayerStack':
            if typed.source != binding:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_LAYER_SOURCE')
        elif contract == 'CorrectionPlan':
            identities, ordinals, matches = set(), set(), 0
            for item in typed.items:
                known = self.images.get(item.binding.asset_version_id)
                if (known is None or known[0] != item.binding or item.binding.asset_version_id in identities
                        or item.ordinal in ordinals):
                    raise AdoptionInvalid('INTERCHANGE_SEMANTICS_CORRECTION_SOURCE')
                identities.add(item.binding.asset_version_id)
                ordinals.add(item.ordinal)
                matches += item.binding == binding
            if matches != 1:
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_CORRECTION_TARGET')
        self.active()
        if self.runtime(contract, deepcopy(typed), binding.model_copy(deep=True)) is not True:
            raise AdoptionInvalid('INTERCHANGE_SEMANTICS_RUNTIME')
        if contract == 'Override':
            self._qualify_confirmed_masks(typed, binding)
        elif contract == 'ResolvedStyle' and typed.params.override is not None:
            self._qualify_confirmed_masks(typed.params.override, binding)
        if self.arrays is not None:
            if contract == 'DefectCandidates':
                self.arrays.register_defect_proposal(binding.model_copy(deep=True), typed.model_copy(deep=True))
            elif contract == 'MaskRef':
                self.arrays.register_mask(binding.model_copy(deep=True), typed.model_copy(deep=True))
            elif contract == 'Params' and isinstance(typed, StatsParams):
                self.arrays.register_mask(binding.model_copy(deep=True), typed.person.model_copy(deep=True))
            elif contract == 'Params' and isinstance(typed, (PortraitParams, DefectRepairParams)):
                masks = ([region.mask for region in typed.regions] + list(typed.protected)
                         if isinstance(typed, PortraitParams) else [typed.protected])
                for mask in masks:
                    self.arrays.register_mask(binding.model_copy(deep=True), mask.model_copy(deep=True))
            elif contract == 'Override':
                for mask_binding in typed.mask_refs:
                    self.arrays.register_mask(binding.model_copy(deep=True), mask_binding.mask.model_copy(deep=True))
            elif contract == 'ResolvedStyle':
                if typed.absolute_lut is not None:
                    self.arrays.register_lut(binding.model_copy(deep=True), typed.model_copy(deep=True))
                if typed.params.override is not None:
                    for mask_binding in typed.params.override.mask_refs:
                        self.arrays.register_mask(binding.model_copy(deep=True), mask_binding.mask.model_copy(deep=True))
        # Array registration can perform object IO. A revoke during that IO
        # must not leave this prepared document qualified.
        if contract == 'Override':
            self._qualify_confirmed_masks(typed, binding)
        elif contract == 'ResolvedStyle' and typed.params.override is not None:
            self._qualify_confirmed_masks(typed.params.override, binding)
        self.active()
        return True

    def _qualify_confirmed_masks(self, override, binding):
        for item in override.mask_refs:
            if not item.confirmed:
                continue
            # Only the newly imported whole-person decision may be supplied by
            # this independent live authority. Other archived confirmations
            # are never inherited, even when their object bytes are identical.
            if (self.confirmation is None or item.mask.selector != 'person'
                    or item.mask.object_id is not None
                    or self.confirmation(item.mask.model_copy(deep=True),
                                         binding.model_copy(deep=True)) is not True):
                raise AdoptionInvalid('INTERCHANGE_SEMANTICS_MASK_CONFIRMATION')
            self.active()


def build_prepared_import_recipe_qualifier(*, canonical_images, work_icc_sha,
        entries, schemas_jcs, schemas_sha, read_document, evidence_reader, scratch,
        max_file_bytes, qualify_runtime, extra_validators=None, check_active=lambda: None, recheck_inputs=None,
        qualify_occurrence=None, operation_schemas=None, qualify_confirmation=None,
        background_mappings=None):
    """Install source semantics and physical arrays into the frozen stage port."""
    from .interchange_recipe_arrays import PreparedImportArrayQualifier
    from .interchange_recipe_closure import PreparedRecipeClosureQualifier
    images = tuple(canonical_images)
    arrays = PreparedImportArrayQualifier(canonical_images=images, reader=evidence_reader,
        scratch=scratch, max_file_bytes=max_file_bytes, check_active=check_active)
    documents = PreparedRecipeDocumentQualifier(canonical_images=images,
        work_icc_sha=work_icc_sha, qualify_runtime=qualify_runtime,
        extra_validators=extra_validators, array_qualifier=arrays, check_active=check_active,
        qualify_confirmation=qualify_confirmation)
    from .interchange_recipe_backgrounds import PreparedImportBackgroundQualifier
    backgrounds = PreparedImportBackgroundQualifier(canonical_images=images, reader=evidence_reader,
        scratch=scratch, check_active=check_active)
    def qualify_binary(reference, contract, binding):
        if contract == 'StyleBackgroundAsset':
            return backgrounds(reference, contract, binding)
        return arrays(reference, contract, binding)
    return PreparedRecipeClosureQualifier(entries=entries, schemas_jcs=schemas_jcs,
        schemas_sha=schemas_sha, read_document=read_document,
        qualify_document=documents, qualify_binary=qualify_binary, check_active=check_active, recheck_inputs=recheck_inputs, qualify_occurrence=qualify_occurrence, operation_schemas=operation_schemas, background_mappings=background_mappings)
