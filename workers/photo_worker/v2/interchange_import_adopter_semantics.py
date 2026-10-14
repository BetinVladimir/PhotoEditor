"""Built-in, deliberately bounded semantic support for imported recipes.

Source geometry and physical arrays are checked by PreparedRecipeDocumentQualifier.
This independent process policy admits only deterministic documents and the
model operations selected by the process and requalified by the live catalog.
Unknown models, review decisions, generators and non-person masks fail closed.
"""

from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.override import OverrideV1
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.restoration import DenoiseParams, UpscaleParams
from photo_worker.contracts.statistics import StatsParams
from photo_worker.contracts.style import Disabled, StyleV2
from photo_worker.operations.session.contracts import CorrectionPlan, VersionBinding
from .render_layers import RenderLayerStack
from .render_recipe import EditingRecipeV3


def _disabled(value):
    return value is None or isinstance(value, Disabled)


def _inactive_extensions(value):
    if value is None:
        return True
    return (all(_disabled(getattr(value, name)) for name in
                ('culling', 'series', 'composition', 'portrait_detail',
                 'inpainting', 'generation'))
            and value.masking is None and value.restoration is None
            and value.background is None and not value.region_adjustments)


def _safe_override(value):
    return (isinstance(value, OverrideV1) and value.object_id is None
            and not value.mask_refs and not value.protected_polygons
            and value.manual_landmarks is None
            and not value.accepted_candidate_ids and not value.rejected_candidate_ids
            and _disabled(value.modules.local) and _disabled(value.modules.beauty)
            and _disabled(value.modules.defects) and _disabled(value.modules.geometry)
            and _inactive_extensions(value.modules.extensions))


class SupportedImportSemanticPolicy:
    """Allow the implemented deterministic subset; defer other operators."""

    def __init__(self, qualifications):
        if not isinstance(qualifications, dict):
            raise ValueError('INTERCHANGE_IMPORT_SEMANTIC_POLICY')
        self.operations = frozenset(qualifications)

    def __call__(self, scope, context, contract, typed, binding):
        if (not isinstance(scope, tuple) or len(scope) != 5
                or getattr(context, 'operation', None) != 'v2.interchange.import'
                or getattr(context, 'version', None) != '1'
                or (getattr(context, 'attempt', None), getattr(context, 'fence', None),
                    getattr(context, 'result_id', None)) != scope[2:]
                or not isinstance(binding, VersionBinding)):
            return False
        if contract == 'WorkingImage':
            return isinstance(typed, WorkingImage)
        if contract == 'EditingRecipeV3':
            return isinstance(typed, EditingRecipeV3) and typed.learning_version_id is None
        if contract == 'CorrectionPlan':
            return isinstance(typed, CorrectionPlan)
        if contract == 'LayerStack':
            return (isinstance(typed, RenderLayerStack)
                    and typed.source == binding
                    and all(layer.operation_version == '1' and (
                        layer.operation == 'image.denoise' and
                        'image.denoise' in self.operations or
                        layer.operation == 'image.upscale' and any(
                            isinstance(key, tuple) and len(key) == 2 and
                            key[0] == 'image.upscale' for key in self.operations))
                        for layer in typed.layers))
        if contract == 'StyleV2':
            return (isinstance(typed, StyleV2) and typed.mode == 'fixed'
                    and typed.quality.status == 'measured' and not typed.models
                    and _disabled(typed.local) and _disabled(typed.beauty)
                    and _disabled(typed.defects) and _disabled(typed.geometry)
                    and _inactive_extensions(typed.extensions))
        if contract == 'Override':
            return _safe_override(typed)
        if contract == 'ResolvedStyle':
            return (isinstance(typed, ResolvedStyle) and typed.mode == 'fixed'
                    and typed.absolute_lut is None and not typed.modules.models
                    and _disabled(typed.local) and _disabled(typed.modules.beauty)
                    and _disabled(typed.modules.defects)
                    and _disabled(typed.modules.geometry)
                    and _inactive_extensions(typed.modules.extensions)
                    and typed.params.job_override is None
                    and (typed.params.override is None or
                         _safe_override(typed.params.override)))
        if contract == 'MaskRef':
            return (isinstance(typed, MaskRef) and typed.selector == 'person'
                    and typed.object_id is None and 'mask.person' in self.operations)
        if contract == 'Params':
            if isinstance(typed, DenoiseParams):
                return 'image.denoise' in self.operations
            if isinstance(typed, UpscaleParams):
                return ('image.upscale', typed.scale) in self.operations
            if isinstance(typed, StatsParams):
                return ('color.stats' in self.operations
                        and 'mask.person' in self.operations)
        return False
