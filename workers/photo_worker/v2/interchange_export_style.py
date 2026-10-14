"""Exact export StyleV2 schema/model pins; no style remapping or scalar edits."""
from hashlib import sha256
import re
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.style import StyleV2


def validate_export_style(raw, expected_sha, installed_models):
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024):
        raise ValueError('INTERCHANGE_EXPORT_STYLE_INPUT')
    installed = dict(installed_models)
    if any(not isinstance(key, str) or not 1 <= len(key) <= 240
           or not isinstance(value, str) or not 1 <= len(value) <= 240
           for key, value in installed.items()):
        raise ValueError('INTERCHANGE_EXPORT_STYLE_MODEL_POLICY')
    parsed = parse_json(raw, max_bytes=8 << 20)
    if canonical_bytes(parsed) != raw:
        raise ValueError('INTERCHANGE_EXPORT_STYLE_CANONICAL')
    # Reuse all processing-quality invariants, including complete curves/LUTs,
    # enabled-module dependencies and the production example-profile refusal.
    style = StyleV2.from_bytes(raw)
    for model in style.models:
        if installed.get(model.id) != model.weight_id:
            raise ValueError('INTERCHANGE_EXPORT_STYLE_MODEL_NOT_INSTALLED')
    return style


def validate_export_override(raw, expected_sha, installed_models):
    """Validate absolute replacement modules without modifying authored values.

    Installed policy is pinned by the probe. Merged StyleV2 model dependencies
    and mask provenance must still be qualified by the scoped export authority.
    """
    from photo_worker.contracts.override import OverrideV1
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024):
        raise ValueError('INTERCHANGE_EXPORT_OVERRIDE_INPUT')
    if any(not isinstance(key, str) or not 1 <= len(key) <= 240
           or not isinstance(value, str) or not 1 <= len(value) <= 240
           for key, value in installed_models.items()):
        raise ValueError('INTERCHANGE_EXPORT_OVERRIDE_POLICY')
    parsed = parse_json(raw, max_bytes=8 << 20)
    if canonical_bytes(parsed) != raw:
        raise ValueError('INTERCHANGE_EXPORT_OVERRIDE_CANONICAL')
    return OverrideV1.model_validate_json(raw)


def validate_export_composition(raw, expected_sha, installed_models):
    """Qualify an ordered, physically pinned bundle using production merging.

    The caller must prove each bundle ref is the corresponding frozen recipe
    edge and recheck SQL/object authority. This function does not grant authority.
    """
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.v2.render_overrides import merge_render_override_modules
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 128 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha):
        raise ValueError('INTERCHANGE_EXPORT_COMPOSITION_INPUT')
    bundle = parse_json(raw, max_bytes=128 << 20)
    if (not isinstance(bundle, dict) or set(bundle) != {'schema_version', 'source', 'base', 'overrides'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or canonical_bytes(bundle) != raw
            or not isinstance(bundle['overrides'], list) or len(bundle['overrides']) > 128):
        raise ValueError('INTERCHANGE_EXPORT_COMPOSITION_ENVELOPE')

    def document(pin):
        if (not isinstance(pin, dict) or set(pin) != {'sha256', 'body'}
                or not isinstance(pin['body'], dict)
                or not isinstance(pin['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', pin['sha256']) is None):
            raise ValueError('INTERCHANGE_EXPORT_COMPOSITION_DOCUMENT')
        body = canonical_bytes(pin['body'])
        if not 2 <= len(body) <= 8 << 20 or sha256(body).hexdigest() != pin['sha256']:
            raise ValueError('INTERCHANGE_EXPORT_COMPOSITION_DOCUMENT_PIN')
        return body

    source_raw = document(bundle['source'])
    source = WorkingImage.model_validate_json(source_raw)
    if (source.dtype != 'float32' or source.color.profile_id != 'work_rec2020_gamma24_v1'
            or source.color.encoding != 'gamma24'):
        raise ValueError('INTERCHANGE_EXPORT_COMPOSITION_SOURCE')
    base_raw = document(bundle['base'])
    base = validate_export_style(base_raw, bundle['base']['sha256'], installed_models)
    if base.color.icc_sha256 != source.color.icc_sha256:
        raise ValueError('INTERCHANGE_EXPORT_COMPOSITION_ICC')
    overrides = tuple(validate_export_override(document(pin), pin['sha256'], installed_models)
                      for pin in bundle['overrides'])
    merged = merge_render_override_modules(base, overrides, source)
    # Re-run installed-model and full production profile checks on the result.
    result_raw = canonical_bytes(merged)
    return validate_export_style(result_raw, sha256(result_raw).hexdigest(), installed_models)


def validate_export_resolved(raw, expected_sha, installed_models):
    """Full processing contract; source/producer/consent remain scoped duties."""
    from photo_worker.contracts.resolved import ResolvedStyle
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024):
        raise ValueError('INTERCHANGE_EXPORT_RESOLVED_INPUT')
    installed = dict(installed_models)
    if any(not isinstance(key, str) or not 1 <= len(key) <= 240
           or not isinstance(value, str) or not 1 <= len(value) <= 240
           for key, value in installed.items()):
        raise ValueError('INTERCHANGE_EXPORT_RESOLVED_POLICY')
    parsed = parse_json(raw, max_bytes=8 << 20)
    if canonical_bytes(parsed) != raw:
        raise ValueError('INTERCHANGE_EXPORT_RESOLVED_CANONICAL')
    resolved = ResolvedStyle.model_validate_json(raw)
    for model in resolved.modules.models:
        if installed.get(model.id) != model.weight_id:
            raise ValueError('INTERCHANGE_EXPORT_RESOLVED_MODEL_NOT_INSTALLED')
    return resolved


def validate_export_resolved_origin(raw, expected_sha, installed_models):
    """Compare original Params using production defaults and exclusion rules.

    The caller must load original Params from the canonical completed producer
    and recheck current SQL/model/consent authority. This only binds contents.
    """
    from photo_worker.contracts.resolved import ResolveParams
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 10 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha):
        raise ValueError('INTERCHANGE_EXPORT_RESOLVED_ORIGIN_INPUT')
    bundle = parse_json(raw, max_bytes=10 << 20)
    if (not isinstance(bundle, dict)
            or set(bundle) != {'schema_version', 'resolved', 'params'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_RESOLVED_ORIGIN_ENVELOPE')
    def document(pin, maximum):
        if (not isinstance(pin, dict) or set(pin) != {'sha256', 'body'}
                or not isinstance(pin['body'], dict)
                or not isinstance(pin['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', pin['sha256']) is None):
            raise ValueError('INTERCHANGE_EXPORT_RESOLVED_ORIGIN_DOCUMENT')
        value = canonical_bytes(pin['body'])
        if not 2 <= len(value) <= maximum or sha256(value).hexdigest() != pin['sha256']:
            raise ValueError('INTERCHANGE_EXPORT_RESOLVED_ORIGIN_PIN')
        return value
    resolved_raw = document(bundle['resolved'], 8 << 20)
    params_raw = document(bundle['params'], 1 << 20)
    resolved = validate_export_resolved(resolved_raw, bundle['resolved']['sha256'], installed_models)
    original = ResolveParams.model_validate_json(params_raw)
    if canonical_bytes(original) != canonical_bytes(resolved.params):
        raise ValueError('INTERCHANGE_EXPORT_RESOLVED_ORIGIN_PARAMS')
    return resolved


def validate_export_operation_params(raw, expected_sha, installed_models):
    """Closed production schema validation; operation comes from SQL authority.

    This does not establish producer identity, source/consent/model eligibility
    or physical mask validity. Those remain independent mandatory checks.
    """
    from photo_worker.contracts.restoration import DenoiseParams, UpscaleParams
    from photo_worker.contracts.image_local import LocalParams
    from photo_worker.contracts.body import BodyParams
    from photo_worker.contracts.statistics import StatsParams
    from photo_worker.contracts.defects import DefectRepairParams
    from photo_worker.contracts.portrait_detail import PortraitParams
    from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 2 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024
            or any(not isinstance(key, str) or not 1 <= len(key) <= 240
                   or not isinstance(value, str) or not 1 <= len(value) <= 240
                   for key, value in installed_models.items())):
        raise ValueError('INTERCHANGE_EXPORT_OPERATION_PARAMS_INPUT')
    bundle = parse_json(raw, max_bytes=2 << 20)
    if (not isinstance(bundle, dict)
            or set(bundle) != {'schema_version', 'operation', 'version', 'params'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or bundle['version'] != '1' or canonical_bytes(bundle) != raw
            or not isinstance(bundle['operation'], str)):
        raise ValueError('INTERCHANGE_EXPORT_OPERATION_PARAMS_ENVELOPE')
    models = {'geometry.body': BodyParams, 'image.local': LocalParams, 'color.stats': StatsParams, 'defects.repair': DefectRepairParams,
              'portrait.detail': PortraitParams, 'portrait.hair': PortraitHairParams,
              'portrait.clothing': PortraitClothingParams, 'image.denoise': DenoiseParams, 'image.upscale': UpscaleParams}
    model = models.get(bundle['operation'])
    if model is None:
        raise ValueError('INTERCHANGE_EXPORT_OPERATION_PARAMS_UNKNOWN')
    pin = bundle['params']
    if (not isinstance(pin, dict) or set(pin) != {'sha256', 'body'}
            or not isinstance(pin['body'], dict) or not isinstance(pin['sha256'], str)
            or re.fullmatch('[0-9a-f]{64}', pin['sha256']) is None):
        raise ValueError('INTERCHANGE_EXPORT_OPERATION_PARAMS_DOCUMENT')
    params_raw = canonical_bytes(pin['body'])
    if not 2 <= len(params_raw) <= 1 << 20 or sha256(params_raw).hexdigest() != pin['sha256']:
        raise ValueError('INTERCHANGE_EXPORT_OPERATION_PARAMS_PIN')
    return model.model_validate_json(params_raw)


def validate_export_series_corrections(raw, expected_sha, installed_models):
    """Full production schema; producer/project qualification is independent."""
    from photo_worker.contracts.series import SeriesCorrections
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024
            or any(not isinstance(k, str) or not 1 <= len(k) <= 240
                   or not isinstance(v, str) or not 1 <= len(v) <= 240
                   for k, v in installed_models.items())):
        raise ValueError('INTERCHANGE_EXPORT_SERIES_INPUT')
    document = parse_json(raw, max_bytes=8 << 20)
    if canonical_bytes(document) != raw:
        raise ValueError('INTERCHANGE_EXPORT_SERIES_CANONICAL')
    return SeriesCorrections.model_validate_json(raw)


def validate_export_series_origin(raw, expected_sha, installed_models):
    """Pinned original series inputs; SQL/current source authority is external."""
    import math
    from uuid import UUID
    from photo_worker.contracts.series import SeriesParams, SeriesStats
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 32 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha):
        raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_INPUT')
    bundle = parse_json(raw, max_bytes=32 << 20)
    if (not isinstance(bundle, dict) or set(bundle) != {'schema_version', 'corrections', 'params', 'stats'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_ENVELOPE')

    def pin(value, *, artifact=False, maximum=1 << 20):
        keys = {'sha256', 'body'} | ({'artifact_id'} if artifact else set())
        if (not isinstance(value, dict) or set(value) != keys or not isinstance(value['body'], dict)
                or not isinstance(value['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', value['sha256']) is None):
            raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_PIN')
        document = canonical_bytes(value['body'])
        if not 2 <= len(document) <= maximum or sha256(document).hexdigest() != value['sha256']:
            raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_BYTES')
        return document

    correction_raw = pin(bundle['corrections'], maximum=8 << 20)
    corrections = validate_export_series_corrections(correction_raw, bundle['corrections']['sha256'], installed_models)
    params = SeriesParams.model_validate_json(pin(bundle['params']))
    if (not isinstance(bundle['stats'], list) or len(bundle['stats']) != len(params.stats)
            or not 2 <= len(bundle['stats']) <= 2000):
        raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_INVENTORY')
    statistics = []
    refs = set()
    for expected, value in zip(params.stats, bundle['stats']):
        document = pin(value, artifact=True)
        identity = UUID(value['artifact_id'])
        if identity.int == 0 or identity != expected.artifact_id or value['sha256'] != expected.sha256 or identity in refs:
            raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_REFERENCE')
        refs.add(identity)
        item = SeriesStats.model_validate_json(document)
        regions = [region.region_id for region in item.regions]
        if (len(regions) != len(set(regions)) or any(not math.isfinite(v) for region in item.regions for v in region.mean_rgb)):
            raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_REGIONS')
        statistics.append(item)
    ids = [item.item_id for item in statistics]
    if (len(ids) != len(set(ids)) or [item.item_id for item in corrections.items] != ids
            or corrections.scene_id != params.scene_id or corrections.reference_item_id not in ids
            or params.reference_item_id is not None and corrections.reference_item_id != params.reference_item_id):
        raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_BINDING')
    common = sorted(set.intersection(*(set(region.region_id for region in item.regions) for item in statistics)))
    if corrections.common_region_ids != common:
        raise ValueError('INTERCHANGE_EXPORT_SERIES_ORIGIN_COMMON_REGIONS')
    return corrections


def validate_export_series_measure_origin(raw, expected_sha, installed_models):
    """Original measurement schema/geometry binding; physical arrays are external."""
    import math
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.series import SeriesMeasureParams, SeriesStats
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 10 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024
            or any(not isinstance(k, str) or not 1 <= len(k) <= 240
                   or not isinstance(v, str) or not 1 <= len(v) <= 240
                   for k, v in installed_models.items())):
        raise ValueError('INTERCHANGE_EXPORT_SERIES_MEASURE_INPUT')
    bundle = parse_json(raw, max_bytes=10 << 20)
    if (not isinstance(bundle, dict) or set(bundle) != {'schema_version', 'stats', 'params', 'image'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_SERIES_MEASURE_ENVELOPE')

    def pin(value, maximum):
        if (not isinstance(value, dict) or set(value) != {'sha256', 'body'}
                or not isinstance(value['body'], dict) or not isinstance(value['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', value['sha256']) is None):
            raise ValueError('INTERCHANGE_EXPORT_SERIES_MEASURE_PIN')
        document = canonical_bytes(value['body'])
        if not 2 <= len(document) <= maximum or sha256(document).hexdigest() != value['sha256']:
            raise ValueError('INTERCHANGE_EXPORT_SERIES_MEASURE_BYTES')
        return document

    stats = SeriesStats.model_validate_json(pin(bundle['stats'], 1 << 20))
    params = SeriesMeasureParams.model_validate_json(pin(bundle['params'], 1 << 20))
    image = WorkingImage.model_validate_json(pin(bundle['image'], 8 << 20))
    if (stats.item_id != params.item_id or stats.image != params.image
            or params.image.sha256 != bundle['image']['sha256']
            or image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
            or [r.region_id for r in stats.regions] != [r.region_id for r in params.regions]):
        raise ValueError('INTERCHANGE_EXPORT_SERIES_MEASURE_BINDING')
    for original, measured in zip(params.regions, stats.regions):
        mask = original.mask
        expected_confidence = min(mask.confidence, min(1., measured.coverage / .01)) if measured.coverage else 0.
        if (mask.protected or mask.canvas != image.canvas or mask.transform_chain != image.transform_chain
                or measured.confidence != expected_confidence
                or any(not math.isfinite(v) or not 0 <= v <= 1 for v in measured.mean_rgb)):
            raise ValueError('INTERCHANGE_EXPORT_SERIES_MEASURE_REGION')
    return params


def validate_export_mask_origin(raw, expected_sha, installed_models):
    """Person/prompt original content proof; current SQL/operator policy is external."""
    from photo_worker.contracts.images import WorkingImage, MaskRef
    from photo_worker.contracts.person import PersonParams, PersonReport
    from photo_worker.contracts.prompt_mask import PromptMaskParams, PromptMaskReport
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 12 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024
            or any(not isinstance(k, str) or not 1 <= len(k) <= 240
                   or not isinstance(v, str) or not 1 <= len(v) <= 240
                   for k, v in installed_models.items())):
        raise ValueError('INTERCHANGE_EXPORT_MASK_ORIGIN_INPUT')
    bundle = parse_json(raw, max_bytes=12 << 20)
    if (not isinstance(bundle, dict) or set(bundle) != {'schema_version', 'operation', 'version', 'root', 'params', 'report', 'image'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or bundle['version'] != '1' or bundle['operation'] not in ('mask.person', 'mask.prompt')
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_MASK_ORIGIN_ENVELOPE')

    def pin(value, maximum):
        if (not isinstance(value, dict) or set(value) != {'sha256', 'body'}
                or not isinstance(value['body'], dict) or not isinstance(value['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', value['sha256']) is None):
            raise ValueError('INTERCHANGE_EXPORT_MASK_ORIGIN_PIN')
        document = canonical_bytes(value['body'])
        if not 2 <= len(document) <= maximum or sha256(document).hexdigest() != value['sha256']:
            raise ValueError('INTERCHANGE_EXPORT_MASK_ORIGIN_BYTES')
        return document

    image = WorkingImage.model_validate_json(pin(bundle['image'], 8 << 20))
    root = MaskRef.model_validate_json(pin(bundle['root'], 1 << 20))
    person = bundle['operation'] == 'mask.person'
    params = (PersonParams if person else PromptMaskParams).model_validate_json(pin(bundle['params'], 1 << 20))
    report = (PersonReport if person else PromptMaskReport).model_validate_json(pin(bundle['report'], 1 << 20))
    if (params.image != report.image or params.image.sha256 != bundle['image']['sha256']
            or image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
            or root.canvas != image.canvas or root.transform_chain != image.transform_chain or root.protected
            or root.model_lock_id != params.model_lock_id
            or report.model_id != params.model_id or report.model_lock_id != params.model_lock_id
            or report.preprocessing_id != params.preprocessing_id
            or installed_models.get(params.model_id) != params.model_lock_id):
        raise ValueError('INTERCHANGE_EXPORT_MASK_ORIGIN_BINDING')
    if person:
        if (root.selector != 'person' or root.object_id is not None
                or report.mask != root.artifact or report.confidence != root.confidence):
            raise ValueError('INTERCHANGE_EXPORT_MASK_ORIGIN_PERSON')
    elif (canonical_bytes(report.mask) != canonical_bytes(root)
          or params.source_sha256 != image.canvas.source_sha256
          or params.object_id != root.object_id or params.selector != root.selector
          or report.score != root.confidence or report.selected_index >= report.candidate_count):
        raise ValueError('INTERCHANGE_EXPORT_MASK_ORIGIN_PROMPT')
    return root


def validate_export_body_origin(raw, expected_sha, installed_models):
    """Pinned production body inputs; SQL/current qualification is independent.

    This verifies source/geometry/model bindings, not numerical body-fit replay.
    """
    from photo_worker.contracts.body import BodyParams
    from photo_worker.contracts.body_fit import BodyFit
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.pose import PoseSet
    from photo_worker.contracts.face import FaceLandmarks
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 10 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024
            or any(not isinstance(k, str) or not 1 <= len(k) <= 240
                   or not isinstance(v, str) or not 1 <= len(v) <= 240
                   for k, v in installed_models.items())):
        raise ValueError('INTERCHANGE_EXPORT_BODY_INPUT')
    bundle = parse_json(raw, max_bytes=10 << 20)
    if (not isinstance(bundle, dict)
            or set(bundle) != {'schema_version', 'params', 'image', 'poses', 'fit', 'faces'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_BODY_ENVELOPE')

    def pinned(value, model, digest=None):
        if (not isinstance(value, dict) or set(value) != {'sha256', 'body'}
                or not isinstance(value['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', value['sha256']) is None
                or not isinstance(value['body'], dict)):
            raise ValueError('INTERCHANGE_EXPORT_BODY_DOCUMENT')
        body = canonical_bytes(value['body'])
        if (not 2 <= len(body) <= 2 << 20 or sha256(body).hexdigest() != value['sha256']
                or digest is not None and value['sha256'] != digest):
            raise ValueError('INTERCHANGE_EXPORT_BODY_PIN')
        return model.model_validate_json(body)

    params = pinned(bundle['params'], BodyParams)
    image = pinned(bundle['image'], WorkingImage, params.image.sha256)
    poses = pinned(bundle['poses'], PoseSet, params.poses.sha256)
    if (image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
            or image.canvas.width * image.canvas.height > 40000000
            or poses.image != params.image
            or params.person.canvas != image.canvas
            or params.person.transform_chain != image.transform_chain
            or installed_models.get(poses.model_id) != poses.model_lock_id
            or any(p.canvas != image.canvas or p.transform_chain != image.transform_chain
                   for p in poses.poses)):
        raise ValueError('INTERCHANGE_EXPORT_BODY_SOURCE')
    ids = [p.object_id for p in poses.poses]
    if len(set(ids)) != len(ids):
        raise ValueError('INTERCHANGE_EXPORT_BODY_OBJECTS')
    if params.faces is None:
        if bundle['faces'] is not None:
            raise ValueError('INTERCHANGE_EXPORT_BODY_UNDECLARED_FACES')
    else:
        faces = pinned(bundle['faces'], FaceLandmarks, params.faces.sha256)
        if (faces.image != params.image
                or installed_models.get(faces.model_id) != faces.model_lock_id
                or any(f.canvas != image.canvas or f.transform_chain != image.transform_chain
                       for f in faces.landmarks)):
            raise ValueError('INTERCHANGE_EXPORT_BODY_FACES')
    if params.fit is None:
        if bundle['fit'] is not None:
            raise ValueError('INTERCHANGE_EXPORT_BODY_UNDECLARED_FIT')
    else:
        fit = pinned(bundle['fit'], BodyFit, params.fit.sha256)
        if (fit.params.before != params.image or fit.params.before_pose != params.poses
                or fit.params.before_person != params.person
                or fit.params.before_faces != params.faces):
            raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_SOURCE')
        selected = [o.before_object_id for o in fit.objects if o.accepted]
        if len(set(selected)) != len(selected) or any(i not in ids for i in selected):
            raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_OBJECTS')
    return params


def validate_export_body_dependency_origin(raw, expected_sha, installed_models):
    """Bind original producer Params to its complete production result schema."""
    from photo_worker.contracts.pose import PoseParams, PoseSet
    from photo_worker.contracts.face import LandmarkParams, FaceLandmarks, FaceDetectParams, FaceBoxes
    from photo_worker.contracts.body_fit import BodyFitParams, BodyFit
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 4 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024
            or any(not isinstance(k, str) or not 1 <= len(k) <= 240
                   or not isinstance(v, str) or not 1 <= len(v) <= 240
                   for k, v in installed_models.items())):
        raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_INPUT')
    bundle = parse_json(raw, max_bytes=4 << 20)
    if (not isinstance(bundle, dict)
            or set(bundle) != {'schema_version', 'operation', 'version', 'params', 'result'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or bundle['version'] != '1' or not isinstance(bundle['operation'], str)
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_ENVELOPE')
    registry = {'pose.detect': (PoseParams, PoseSet),
                'face.landmarks': (LandmarkParams, FaceLandmarks),
                'face.detect': (FaceDetectParams, FaceBoxes),
                'pair.fit.body': (BodyFitParams, BodyFit)}
    models = registry.get(bundle['operation'])
    if models is None:
        raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_OPERATION')
    parsed = []
    for key, model, maximum in zip(('params', 'result'), models, (1 << 20, 2 << 20)):
        pin = bundle[key]
        if (not isinstance(pin, dict) or set(pin) != {'sha256', 'body'}
                or not isinstance(pin['body'], dict) or not isinstance(pin['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', pin['sha256']) is None):
            raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_DOCUMENT')
        body = canonical_bytes(pin['body'])
        if not 2 <= len(body) <= maximum or sha256(body).hexdigest() != pin['sha256']:
            raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_PIN')
        parsed.append(model.model_validate_json(body))
    params, result = parsed
    if bundle['operation'] == 'pair.fit.body':
        if result.params != params:
            raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_FIT_PARAMS')
    else:
        if (result.image != params.image or result.model_id != params.model_id
                or result.model_lock_id != params.model_lock_id
                or result.preprocessing_id != params.preprocessing_id
                or installed_models.get(params.model_id) != params.model_lock_id):
            raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_MODEL_SOURCE')
        if bundle['operation'] == 'face.landmarks' and result.faces != params.faces:
            raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_FACES')
        if bundle['operation'] == 'pose.detect' and (
                result.accepted_count != len(result.poses)
                or result.detected_count < result.accepted_count
                or len(result.poses) > params.max_people):
            raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_POSE_COUNT')
    if bundle['operation'] == 'face.detect' and (
            result.accepted_count != len(result.faces)
            or result.candidate_count < result.accepted_count
            or len(result.faces) > params.max_faces):
        raise ValueError('INTERCHANGE_EXPORT_BODY_DEPENDENCY_FACE_COUNT')
    return result


def validate_export_body_fit_origin(raw, expected_sha, installed_models):
    """Full paired input content/geometry; producer policy and bytes are external."""
    from photo_worker.contracts.body_fit import BodyFit
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.pair import PairAlignment
    from photo_worker.contracts.pose import PoseSet
    from photo_worker.contracts.face import FaceLandmarks
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 16 << 20
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024
            or any(not isinstance(k, str) or not 1 <= len(k) <= 240
                   or not isinstance(v, str) or not 1 <= len(v) <= 240
                   for k, v in installed_models.items())):
        raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_INPUT')
    bundle = parse_json(raw, max_bytes=16 << 20)
    fields = {'schema_version', 'fit', 'before', 'after', 'alignment',
              'before_pose', 'after_pose', 'before_faces', 'after_faces'}
    if (not isinstance(bundle, dict) or set(bundle) != fields
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_ENVELOPE')
    def pinned(value, model, digest=None):
        if (not isinstance(value, dict) or set(value) != {'sha256', 'body'}
                or not isinstance(value['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', value['sha256']) is None
                or not isinstance(value['body'], dict)):
            raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_DOCUMENT')
        body = canonical_bytes(value['body'])
        if (not 2 <= len(body) <= 2 << 20 or sha256(body).hexdigest() != value['sha256']
                or digest is not None and value['sha256'] != digest):
            raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_PIN')
        return model.model_validate_json(body)
    fit = pinned(bundle['fit'], BodyFit)
    params = fit.params
    images, poses = {}, {}
    for side in ('before', 'after'):
        ref = getattr(params, side)
        image = pinned(bundle[side], WorkingImage, ref.sha256)
        pose = pinned(bundle[side + '_pose'], PoseSet, getattr(params, side + '_pose').sha256)
        mask = getattr(params, side + '_person')
        if (image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
                or image.canvas.width * image.canvas.height > 40000000
                or pose.image != ref or mask.canvas != image.canvas
                or mask.transform_chain != image.transform_chain
                or mask.model_lock_id is None
                or installed_models.get('lraspp_mobilenet_v3_large') != mask.model_lock_id
                or installed_models.get(pose.model_id) != pose.model_lock_id
                or pose.accepted_count != len(pose.poses) or pose.detected_count < pose.accepted_count
                or any(p.canvas != image.canvas or p.transform_chain != image.transform_chain for p in pose.poses)):
            raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_SOURCE')
        ids = [p.object_id for p in pose.poses]
        if len(set(ids)) != len(ids):
            raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_POSES')
        faces_ref = getattr(params, side + '_faces')
        if faces_ref is None:
            if bundle[side + '_faces'] is not None:
                raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_UNDECLARED_FACES')
        else:
            faces = pinned(bundle[side + '_faces'], FaceLandmarks, faces_ref.sha256)
            if (faces.image != ref or installed_models.get(faces.model_id) != faces.model_lock_id
                    or any(f.canvas != image.canvas or f.transform_chain != image.transform_chain for f in faces.landmarks)):
                raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_FACES')
        images[side], poses[side] = image, set(ids)
    alignment = pinned(bundle['alignment'], PairAlignment, params.alignment.sha256)
    if (alignment.crop_aware or alignment.before != params.before or alignment.after != params.after
            or alignment.before_canvas != images['before'].canvas
            or alignment.after_canvas != images['after'].canvas):
        raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_ALIGNMENT')
    accepted = [o for o in fit.objects if o.accepted]
    for side in ('before', 'after'):
        ids = [getattr(o, side + '_object_id') for o in accepted]
        if len(set(ids)) != len(ids) or any(i not in poses[side] for i in ids):
            raise ValueError('INTERCHANGE_EXPORT_BODY_FIT_OBJECTS')
    return fit


def validate_export_pair_alignment_origin(raw, expected_sha, installed_models):
    """Pinned original registration inputs/result; causal authority is external."""
    from photo_worker.contracts.pair import RegistrationParams, PairAlignment, validate_alignment_origin
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 4 << 20
            or not isinstance(expected_sha, str) or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha
            or not isinstance(installed_models, dict) or len(installed_models) > 1024
            or any(not isinstance(k, str) or not 1 <= len(k) <= 240
                   or not isinstance(v, str) or not 1 <= len(v) <= 240
                   for k, v in installed_models.items())):
        raise ValueError('INTERCHANGE_EXPORT_ALIGNMENT_INPUT')
    bundle = parse_json(raw, max_bytes=4 << 20)
    if (not isinstance(bundle, dict)
            or set(bundle) != {'schema_version', 'operation', 'version', 'params', 'result'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or bundle['operation'] != 'pair.register' or bundle['version'] != '1'
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_ALIGNMENT_ENVELOPE')
    parsed = []
    for key, model, maximum in (('params', RegistrationParams, 1 << 20),
                                ('result', PairAlignment, 2 << 20)):
        pin = bundle[key]
        if (not isinstance(pin, dict) or set(pin) != {'sha256', 'body'}
                or not isinstance(pin['body'], dict) or not isinstance(pin['sha256'], str)
                or re.fullmatch('[0-9a-f]{64}', pin['sha256']) is None):
            raise ValueError('INTERCHANGE_EXPORT_ALIGNMENT_DOCUMENT')
        body = canonical_bytes(pin['body'])
        if not 2 <= len(body) <= maximum or sha256(body).hexdigest() != pin['sha256']:
            raise ValueError('INTERCHANGE_EXPORT_ALIGNMENT_PIN')
        parsed.append(model.model_validate_json(body))
    return validate_alignment_origin(*parsed)


def validate_export_face_detector_origin(raw, expected_sha, installed_models):
    """Actual source/preview/face identities; inference replay remains external."""
    from uuid import uuid5, NAMESPACE_URL
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.face import FaceDetectParams
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20
            or not isinstance(expected_sha, str) or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or sha256(raw).hexdigest() != expected_sha):
        raise ValueError('INTERCHANGE_EXPORT_DETECTOR_INPUT')
    bundle = parse_json(raw, max_bytes=8 << 20)
    if (not isinstance(bundle, dict) or set(bundle) != {'schema_version', 'params', 'result', 'image'}
            or type(bundle['schema_version']) is not int or bundle['schema_version'] != 1
            or canonical_bytes(bundle) != raw):
        raise ValueError('INTERCHANGE_EXPORT_DETECTOR_ENVELOPE')
    dependency = canonical_bytes(dict(schema_version=1, operation='face.detect', version='1',
                                     params=bundle['params'], result=bundle['result']))
    result = validate_export_body_dependency_origin(dependency, sha256(dependency).hexdigest(), installed_models)
    params = FaceDetectParams.model_validate_json(canonical_bytes(bundle['params']['body']))
    pin = bundle['image']
    if (not isinstance(pin, dict) or set(pin) != {'sha256', 'body'}
            or not isinstance(pin['body'], dict) or pin['sha256'] != params.image.sha256):
        raise ValueError('INTERCHANGE_EXPORT_DETECTOR_IMAGE')
    image_raw = canonical_bytes(pin['body'])
    if not 2 <= len(image_raw) <= 2 << 20 or sha256(image_raw).hexdigest() != pin['sha256']:
        raise ValueError('INTERCHANGE_EXPORT_DETECTOR_IMAGE_PIN')
    image = WorkingImage.model_validate_json(image_raw)
    if (image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
            or image.canvas.width * image.canvas.height > 40000000
            or image.alpha is None and result.alpha_excluded_fraction != 0):
        raise ValueError('INTERCHANGE_EXPORT_DETECTOR_SOURCE')
    scale = min(1., params.long_side / max(image.canvas.width, image.canvas.height))
    if (result.preview_width != max(1, round(image.canvas.width * scale))
            or result.preview_height != max(1, round(image.canvas.height * scale))):
        raise ValueError('INTERCHANGE_EXPORT_DETECTOR_PREVIEW')
    keys = []
    for ordinal, face in enumerate(result.faces):
        rect = face.bbox_normalized
        signature = ','.join(f'{v:.6f}' for v in (rect.x0, rect.y0, rect.x1, rect.y1))
        expected = uuid5(NAMESPACE_URL, f'face:{image.canvas.source_sha256}:{params.model_lock_id}:{ordinal}:{signature}')
        if face.object_id != expected or face.confidence < params.score_threshold:
            raise ValueError('INTERCHANGE_EXPORT_DETECTOR_FACE_IDENTITY')
        keys.append((rect.y0, rect.x0, rect.y1, rect.x1, -face.confidence))
    if keys != sorted(keys) or len({f.object_id for f in result.faces}) != len(result.faces):
        raise ValueError('INTERCHANGE_EXPORT_DETECTOR_FACE_ORDER')
    return result
