from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.interchange.export_style_probe import probe_export_style
from test_interchange_export_style import style_fixture


@pytest.mark.parametrize('mode', ['installed', 'missing', 'relative', 'corrupt'])
def test_composition_probe_resolves_trusted_physical_icc(tmp_path, monkeypatch, mode):
    from pathlib import Path
    from shutil import copytree
    from photo_worker.color.icc import ICCError
    from test_interchange_export_style import composition_fixture
    value, models = composition_fixture()
    raw = canonical_bytes(value)
    policy = canonical_bytes({'style_weights': models, 'operation_locks': {'mobilesam': 'a' * 64}})
    document = tmp_path / 'composition.json'; document.write_bytes(raw)
    policy_path = tmp_path / 'composition-policy.json'; policy_path.write_bytes(policy)
    request = dict(schema_version=3, contract='RenderComposition', file_path=str(document),
                   bytes=len(raw), sha256=sha256(raw).hexdigest(),
                   model_policy_path=str(policy_path), model_policy_bytes=len(policy),
                   model_policy_sha256=sha256(policy).hexdigest())
    root = tmp_path / 'icc'
    copytree(Path(__file__).resolve().parents[2] / 'resources' / 'icc', root)
    if mode == 'missing': root = tmp_path / 'absent'
    if mode == 'corrupt': (root / 'work_rec2020_gamma24_v1.icc').write_bytes(b'corrupt')
    monkeypatch.setenv('PHOTO_ICC_ROOT', 'relative' if mode == 'relative' else str(root))
    if mode == 'installed':
        assert probe_export_style(request)['contract'] == 'RenderComposition'
    else:
        with pytest.raises((ValueError, OSError, ICCError)):
            probe_export_style(request)


def request_for(tmp_path):
    value = style_fixture()
    raw = canonical_bytes(value)
    policy = canonical_bytes({model['id']: model['weight_id'] for model in value['models']})
    path = tmp_path / 'style.json';path.write_bytes(raw)
    policy_path = tmp_path / 'policy.json';policy_path.write_bytes(policy)
    return dict(schema_version=1, file_path=str(path), bytes=len(raw), sha256=sha256(raw).hexdigest(),
        model_policy_path=str(policy_path), model_policy_bytes=len(policy),
        model_policy_sha256=sha256(policy).hexdigest())


def test_style_probe_binds_actual_style_and_model_policy(tmp_path):
    request = request_for(tmp_path)
    result = probe_export_style(request)
    assert result['contract'] == 'StyleV2' and result['sha256'] == request['sha256']
    assert result['model_policy_sha256'] == request['model_policy_sha256']
    assert len(result['runtime_sha256']) == 64 and 'file_path' not in result


@pytest.mark.parametrize('mode', ['style_hash', 'policy_hash', 'size', 'symlink', 'unknown', 'boolean'])
def test_style_probe_rejects_bad_pins(tmp_path, mode):
    request = request_for(tmp_path)
    if mode == 'style_hash': request['sha256'] = 'a' * 64
    if mode == 'policy_hash': request['model_policy_sha256'] = 'b' * 64
    if mode == 'size': request['bytes'] += 1
    if mode == 'unknown': request['approved'] = True
    if mode == 'boolean': request['schema_version'] = True
    if mode == 'symlink':
        link = tmp_path / 'link.json';link.symlink_to(request['file_path']);request['file_path'] = str(link)
    with pytest.raises((ValueError, OSError)): probe_export_style(request)


@pytest.mark.parametrize('mode', ['valid', 'numeric', 'model', 'hash', 'contract'])
def test_resolved_probe_uses_complete_processing_contract(tmp_path, mode):
    import json
    from pathlib import Path
    from photo_worker.contracts.base import canonical_hash
    fixture = Path(__file__).resolve().parents[2] / 'backend/internal/review/testdata/resolved-binding-python.json'
    value = json.loads(fixture.read_bytes())
    policy = {}
    if mode == 'numeric': value['strength'] = 2
    if mode == 'model': value['modules']['models'] = [dict(id='missing', weight_id='weights')]
    if mode in ('numeric', 'model'):
        value['hash'] = canonical_hash({key: item for key, item in value.items() if key != 'hash'})
    raw = canonical_bytes(value)
    path = tmp_path / 'resolved.json'; path.write_bytes(raw)
    policy_raw = canonical_bytes(policy)
    policy_path = tmp_path / 'policy.json'; policy_path.write_bytes(policy_raw)
    request = dict(schema_version=4, contract='ResolvedStyle', file_path=str(path), bytes=len(raw),
        sha256=sha256(raw).hexdigest(), model_policy_path=str(policy_path),
        model_policy_bytes=len(policy_raw), model_policy_sha256=sha256(policy_raw).hexdigest())
    if mode == 'hash': request['sha256'] = 'a' * 64
    if mode == 'contract': request['contract'] = 'StyleV2'
    if mode == 'valid':
        result = probe_export_style(request)
        assert result['contract'] == 'ResolvedStyle' and result['sha256'] == request['sha256']
    else:
        with pytest.raises(ValueError): probe_export_style(request)


@pytest.mark.parametrize('mode', ['valid', 'defaults', 'strict_ood', 'source', 'params_hash', 'unknown', 'boolean'])
def test_resolved_origin_probe_binds_original_normalized_params(tmp_path, mode):
    import json
    from pathlib import Path
    fixture = Path(__file__).resolve().parents[2] / 'backend/internal/review/testdata/resolved-binding-python.json'
    resolved = json.loads(fixture.read_bytes())
    params = json.loads(json.dumps(resolved['params']))
    if mode == 'defaults':
        if params.get('strict_ood') is False: params.pop('strict_ood', None)
        params['identity'] = False
        if params.get('stats') is None: params.pop('stats', None)
        if params.get('group_prototype_id') is None: params.pop('group_prototype_id', None)
    if mode == 'strict_ood': params['strict_ood'] = not params.get('strict_ood', False)
    if mode == 'source': params['image']['sha256'] = 'f' * 64
    def pin(value):
        return dict(body=value, sha256=sha256(canonical_bytes(value)).hexdigest())
    bundle = dict(schema_version=1, resolved=pin(resolved), params=pin(params))
    if mode == 'params_hash': bundle['params']['sha256'] = 'e' * 64
    if mode == 'unknown': bundle['approved'] = True
    if mode == 'boolean': bundle['schema_version'] = True
    raw = canonical_bytes(bundle)
    path = tmp_path / 'origin.json'; path.write_bytes(raw)
    policy_raw = canonical_bytes({}); policy_path = tmp_path / 'policy.json'; policy_path.write_bytes(policy_raw)
    request = dict(schema_version=5, contract='ResolvedOrigin', file_path=str(path), bytes=len(raw),
        sha256=sha256(raw).hexdigest(), model_policy_path=str(policy_path),
        model_policy_bytes=len(policy_raw), model_policy_sha256=sha256(policy_raw).hexdigest())
    if mode in ('valid', 'defaults'):
        receipt = probe_export_style(request)
        assert receipt['contract'] == 'ResolvedOrigin' and receipt['sha256'] == request['sha256']
    else:
        with pytest.raises(ValueError): probe_export_style(request)


@pytest.mark.parametrize('operation', ['color.stats', 'defects.repair', 'portrait.detail', 'portrait.hair', 'portrait.clothing', 'image.local', 'geometry.body'])
@pytest.mark.parametrize('mode', ['valid', 'unknown_operation', 'params_hash', 'unknown_field', 'boolean_schema', 'version'])
def test_operation_params_probe_uses_production_schemas(tmp_path, operation, mode):
    from uuid import uuid4
    from test_interchange_export_style import composition_fixture
    source = composition_fixture()[0]['source']['body']
    ref = dict(artifact_id=str(uuid4()), sha256='a' * 64)
    mask = dict(artifact=source['pixels'], canvas=source['canvas'],
                transform_chain=source['transform_chain'], object_id=None,
                selector='person', confidence=1, model_lock_id=None, protected=False)
    if operation in ('portrait.hair', 'portrait.clothing'):
        params = dict(schema_version=1, base=dict(asset_version_id=str(uuid4()), source=ref,
                      canvas_hash='b' * 64), strength=1, mask=ref, protected=ref,
                      hue_delta_degrees=0, saturation_factor=1)
        params['detail' if operation == 'portrait.hair' else 'wrinkle_strength'] = .5
    elif operation == 'geometry.body':
        params = dict(image=ref, poses=ref, person=mask,
                      coefficients=dict(face_slim=0, waist_slim=0, hip_slim=0, torso_slim=0, leg_length=0))
    elif operation == 'image.local':
        params = dict(image=ref, base_aligned=ref, resolved=ref, person=mask, regions=[])
    elif operation == 'color.stats':
        params = dict(image=ref, person=mask)
    elif operation == 'defects.repair':
        mask['protected'] = True
        params = dict(image=ref, proposal=ref, review=ref, resolved=ref, protected=mask)
    else:
        params = dict(image=ref, resolved=ref, regions=[])
    if mode == 'unknown_field': params['approved'] = True
    params_raw = canonical_bytes(params)
    bundle = dict(schema_version=1, operation=operation, version='1',
                  params=dict(sha256=sha256(params_raw).hexdigest(), body=params))
    if mode == 'unknown_operation': bundle['operation'] = 'unknown.operation'
    if mode == 'params_hash': bundle['params']['sha256'] = 'f' * 64
    if mode == 'boolean_schema': bundle['schema_version'] = True
    if mode == 'version': bundle['version'] = '2'
    raw = canonical_bytes(bundle)
    path = tmp_path / 'params-bundle.json'; path.write_bytes(raw)
    policy_raw = canonical_bytes({}); policy_path = tmp_path / 'policy.json'; policy_path.write_bytes(policy_raw)
    request = dict(schema_version=6, contract='OperationParams', file_path=str(path), bytes=len(raw),
        sha256=sha256(raw).hexdigest(), model_policy_path=str(policy_path),
        model_policy_bytes=len(policy_raw), model_policy_sha256=sha256(policy_raw).hexdigest())
    if mode == 'valid':
        assert probe_export_style(request)['contract'] == 'OperationParams'
    else:
        with pytest.raises(ValueError): probe_export_style(request)


@pytest.mark.parametrize('mode', ['valid', 'unknown', 'gain', 'skip_identity', 'hash'])
def test_export_series_corrections_production_schema(mode):
    from uuid import uuid4
    from photo_worker.v2.interchange_export_style import validate_export_series_corrections
    item = dict(item_id=str(uuid4()), raw_ev=0, raw_gains=[1, 1, 1],
                applied_ev=0, applied_gains=[1, 1, 1], confidence=1,
                accepted=True, reason='reference')
    other = dict(item, item_id=str(uuid4()))
    document = dict(scene_id='scene', reference_item_id=item['item_id'],
                    common_region_ids=[], items=[item, other], warnings=[])
    if mode == 'unknown': document['approved'] = True
    if mode == 'gain': item['applied_gains'] = [1.1, 1, 1]
    if mode == 'skip_identity': item.update(accepted=False, applied_ev=.1, reason='low_confidence')
    raw = canonical_bytes(document)
    digest = '0' * 64 if mode == 'hash' else sha256(raw).hexdigest()
    if mode == 'valid':
        assert len(validate_export_series_corrections(raw, digest, {}).items) == 2
    else:
        with pytest.raises(ValueError): validate_export_series_corrections(raw, digest, {})


@pytest.mark.parametrize('mode', ['valid', 'scene', 'pin', 'missing', 'order', 'duplicate_item', 'reference', 'common', 'unknown'])
def test_export_series_origin_binds_exact_original_inputs(mode):
    from uuid import uuid4
    from photo_worker.v2.interchange_export_style import validate_export_series_origin
    def pin(body, **extra):
        return dict(sha256=sha256(canonical_bytes(body)).hexdigest(), body=body, **extra)
    image = dict(artifact_id=str(uuid4()), sha256='a' * 64)
    stats = [pin(dict(item_id=str(uuid4()), image=image, regions=[dict(region_id='skin', mean_rgb=[.2, .3, .4], mean_y=.3, coverage=.5, confidence=1)], clipping_fraction=0, warnings=[]), artifact_id=str(uuid4())) for _ in range(2)]
    if mode == 'duplicate_item':
        stats[1]['body']['item_id'] = stats[0]['body']['item_id']
        stats[1]['sha256'] = sha256(canonical_bytes(stats[1]['body'])).hexdigest()
    params = dict(scene_id='scene', stats=[{k: v[k] for k in ('artifact_id', 'sha256')} for v in stats], reference_item_id=stats[0]['body']['item_id'])
    items = [dict(item_id=value['body']['item_id'], raw_ev=0, raw_gains=[1, 1, 1], applied_ev=0, applied_gains=[1, 1, 1], confidence=1, accepted=True, reason='reference') for value in stats]
    corrections = dict(scene_id='scene', reference_item_id=params['reference_item_id'], common_region_ids=['skin'], items=items, warnings=[])
    if mode == 'scene': corrections['scene_id'] = 'other'
    if mode == 'order': corrections['items'] = items[::-1]
    if mode == 'reference': corrections['reference_item_id'] = stats[1]['body']['item_id']
    if mode == 'common': corrections['common_region_ids'] = []
    bundle = dict(schema_version=1, corrections=pin(corrections), params=pin(params), stats=stats)
    if mode == 'pin': stats[0]['sha256'] = 'f' * 64
    if mode == 'missing': bundle['stats'] = stats[:1]
    if mode == 'unknown': bundle['approved'] = True
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        assert len(validate_export_series_origin(raw, sha256(raw).hexdigest(), {}).items) == 2
    else:
        with pytest.raises(ValueError): validate_export_series_origin(raw, sha256(raw).hexdigest(), {})


@pytest.mark.parametrize('mode', ['valid', 'item', 'region', 'confidence', 'protected', 'canvas', 'pin', 'unknown'])
def test_series_measure_origin_uses_original_params_geometry(mode):
    from uuid import uuid4
    from copy import deepcopy
    from test_interchange_export_style import composition_fixture
    from photo_worker.v2.interchange_export_style import validate_export_series_measure_origin
    def pin(body):
        return dict(sha256=sha256(canonical_bytes(body)).hexdigest(), body=body)
    image = deepcopy(composition_fixture()[0]['source']['body'])
    image_pin = pin(image)
    ref = dict(artifact_id=str(uuid4()), sha256=image_pin['sha256'])
    mask = dict(artifact=image['pixels'], canvas=deepcopy(image['canvas']),
                transform_chain=image['transform_chain'], object_id=None,
                selector='person', confidence=1, model_lock_id=None, protected=False)
    item = str(uuid4())
    params = dict(item_id=item, image=ref, regions=[dict(region_id='person', mask=mask)])
    stats = dict(item_id=item, image=ref, regions=[dict(region_id='person', mean_rgb=[.2, .3, .4], mean_y=.3, coverage=.5, confidence=1)], clipping_fraction=0, warnings=[])
    if mode == 'item': stats['item_id'] = str(uuid4())
    if mode == 'region': stats['regions'][0]['region_id'] = 'other'
    if mode == 'confidence': stats['regions'][0]['confidence'] = .5
    if mode == 'protected': mask['protected'] = True
    if mode == 'canvas': mask['canvas']['width'] += 1
    bundle = dict(schema_version=1, stats=pin(stats), params=pin(params), image=image_pin)
    if mode == 'pin': bundle['params']['sha256'] = 'f' * 64
    if mode == 'unknown': bundle['approved'] = True
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        assert len(validate_export_series_measure_origin(raw, sha256(raw).hexdigest(), {}).regions) == 1
    else:
        with pytest.raises(ValueError): validate_export_series_measure_origin(raw, sha256(raw).hexdigest(), {})


@pytest.mark.parametrize('operation', ['mask.person', 'mask.prompt'])
@pytest.mark.parametrize('mode', ['valid', 'policy', 'confidence', 'canvas', 'image', 'unknown', 'operation'])
def test_mask_origin_binds_manifest_original_params_and_report(operation, mode):
    from uuid import uuid4
    from copy import deepcopy
    from test_interchange_export_style import composition_fixture
    from photo_worker.v2.interchange_export_style import validate_export_mask_origin
    def pin(body): return dict(sha256=sha256(canonical_bytes(body)).hexdigest(), body=body)
    image = deepcopy(composition_fixture()[0]['source']['body']); image_pin = pin(image)
    ref = dict(artifact_id=str(uuid4()), sha256=image_pin['sha256']); lock = 'a' * 64
    root = dict(artifact=image['pixels'], canvas=deepcopy(image['canvas']), transform_chain=image['transform_chain'],
                selector='person', confidence=.9, model_lock_id=lock, object_id=None, protected=False)
    if operation == 'mask.person':
        params = dict(image=ref, model_lock_id=lock)
        model = 'lraspp_mobilenet_v3_large'
        report = dict(image=ref, mask=root['artifact'], preview_width=1, preview_height=1,
                      model_id=model, model_lock_id=lock, backend='cpu', preprocessing_id='person_guided_v1',
                      confidence=.9, person_fraction=.5, source_clip_fraction=0, alpha_excluded_fraction=0, warnings=[])
    else:
        params = dict(image=ref, source_sha256=image['canvas']['source_sha256'], selector='person',
                      points=[dict(x=.5, y=.5, label='positive')], model_lock_id=lock)
        model = 'mobilesam'
        report = dict(image=ref, mask=deepcopy(root), candidate_count=1, selected_index=0, score=.9,
                      model_id=model, model_lock_id=lock, backend='cpu', preprocessing_id='mobilesam_long1024_v1', warnings=[])
    policy = {model: lock}
    if mode == 'policy': policy = {}
    if mode == 'confidence': root['confidence'] = .8
    if mode == 'canvas': root['canvas']['width'] += 1
    if mode == 'image': params['image'] = dict(ref, artifact_id=str(uuid4()))
    bundle = dict(schema_version=1, operation=operation, version='1', root=pin(root), params=pin(params), report=pin(report), image=image_pin)
    if mode == 'unknown': bundle['approved'] = True
    if mode == 'operation': bundle['operation'] = 'mask.unknown'
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        assert validate_export_mask_origin(raw, sha256(raw).hexdigest(), policy).confidence == .9
    else:
        with pytest.raises(ValueError): validate_export_mask_origin(raw, sha256(raw).hexdigest(), policy)


@pytest.mark.parametrize('mode', ['valid', 'both_sources', 'no_source', 'unsafe_strength', 'unsafe_displacement', 'unprotected_background', 'selector', 'unknown_algorithm'])
def test_export_body_params_production_safety(mode):
    from uuid import uuid4
    from test_interchange_export_style import composition_fixture
    from photo_worker.v2.interchange_export_style import validate_export_operation_params
    source = composition_fixture()[0]['source']['body']
    ref = dict(artifact_id=str(uuid4()), sha256='a' * 64)
    mask = dict(artifact=source['pixels'], canvas=source['canvas'],
                transform_chain=source['transform_chain'], object_id=None,
                selector='person', confidence=1, model_lock_id=None, protected=False)
    params = dict(image=ref, poses=ref, person=mask,
                  coefficients=dict(face_slim=0, waist_slim=0, hip_slim=0, torso_slim=0, leg_length=0))
    if mode == 'both_sources': params['fit'] = ref
    if mode == 'no_source': del params['coefficients']
    if mode == 'unsafe_strength': params['strength'] = 2
    if mode == 'unsafe_displacement': params['max_displacement_image_fraction'] = .03
    if mode == 'unprotected_background': params['protect_background'] = False
    if mode == 'selector': mask['selector'] = 'manual'
    if mode == 'unknown_algorithm': params['algorithm'] = 'unknown'
    body = canonical_bytes(params)
    raw = canonical_bytes(dict(schema_version=1, operation='geometry.body', version='1',
                               params=dict(sha256=sha256(body).hexdigest(), body=params)))
    if mode == 'valid':
        assert validate_export_operation_params(raw, sha256(raw).hexdigest(), {}).algorithm == 'bodywarp_v1'
    else:
        with pytest.raises(ValueError):
            validate_export_operation_params(raw, sha256(raw).hexdigest(), {})


@pytest.mark.parametrize('mode', ['valid', 'pose_source', 'person_canvas', 'model_policy', 'image_pin', 'unknown_field', 'boolean_schema', 'undeclared_fit', 'missing_faces'])
def test_export_body_origin_source_and_policy(tmp_path, mode):
    from uuid import uuid4
    from test_interchange_export_style import composition_fixture
    source = composition_fixture()[0]['source']['body']
    def pin(body):
        return dict(sha256=sha256(canonical_bytes(body)).hexdigest(), body=body)
    image_pin = pin(source)
    image_ref = dict(artifact_id=str(uuid4()), sha256=image_pin['sha256'])
    pose = dict(image=image_ref, poses=[], model_id='keypointrcnn_resnet50_fpn',
                model_lock_id='c' * 64, backend='mps', preprocessing_id='keypointrcnn_coco_v1',
                detected_count=0, accepted_count=0, alpha_excluded_fraction=0, warnings=[])
    if mode == 'pose_source': pose['image'] = dict(image_ref, artifact_id=str(uuid4()))
    pose_pin = pin(pose)
    mask = dict(artifact=source['pixels'], canvas=dict(source['canvas']),
                transform_chain=source['transform_chain'], object_id=None,
                selector='person', confidence=1, model_lock_id=None, protected=False)
    if mode == 'person_canvas': mask['canvas']['width'] += 1
    params = dict(image=image_ref, poses=dict(artifact_id=str(uuid4()), sha256=pose_pin['sha256']),
                  person=mask, coefficients=dict(face_slim=0, waist_slim=0, hip_slim=0, torso_slim=0, leg_length=0))
    if mode == 'missing_faces': params['faces'] = dict(artifact_id=str(uuid4()), sha256='d' * 64)
    bundle = dict(schema_version=1, params=pin(params), image=image_pin, poses=pose_pin, fit=None, faces=None)
    if mode == 'image_pin': bundle['image']['sha256'] = 'f' * 64
    if mode == 'unknown_field': bundle['approved'] = True
    if mode == 'boolean_schema': bundle['schema_version'] = True
    if mode == 'undeclared_fit': bundle['fit'] = pin({})
    policy = {'keypointrcnn_resnet50_fpn': 'c' * 64}
    if mode == 'model_policy': policy['keypointrcnn_resnet50_fpn'] = 'e' * 64
    raw = canonical_bytes(bundle); path = tmp_path / 'body.json'; path.write_bytes(raw)
    policy_raw = canonical_bytes(policy); policy_path = tmp_path / 'policy.json'; policy_path.write_bytes(policy_raw)
    request = dict(schema_version=11, contract='BodyOrigin', file_path=str(path), bytes=len(raw),
                   sha256=sha256(raw).hexdigest(), model_policy_path=str(policy_path),
                   model_policy_bytes=len(policy_raw), model_policy_sha256=sha256(policy_raw).hexdigest())
    if mode == 'valid':
        assert probe_export_style(request)['contract'] == 'BodyOrigin'
    else:
        with pytest.raises(ValueError): probe_export_style(request)


@pytest.mark.parametrize('operation', ['pose.detect', 'face.landmarks', 'pair.fit.body'])
@pytest.mark.parametrize('mode', ['valid', 'source', 'params_pin', 'result_pin', 'unknown_field', 'version', 'boolean_schema'])
def test_export_body_dependency_original_params(operation, mode):
    from copy import deepcopy
    from uuid import uuid4
    from test_interchange_export_style import composition_fixture
    from photo_worker.v2.interchange_export_style import validate_export_body_dependency_origin
    ref = dict(artifact_id=str(uuid4()), sha256='a' * 64)
    other = dict(ref, artifact_id=str(uuid4()))
    policy = {}
    if operation == 'pose.detect':
        params = dict(image=ref, model_lock_id='c' * 64)
        result = dict(image=ref, poses=[], model_id='keypointrcnn_resnet50_fpn',
                      model_lock_id='c' * 64, backend='mps', preprocessing_id='keypointrcnn_coco_v1',
                      detected_count=0, accepted_count=0, alpha_excluded_fraction=0, warnings=[])
        policy['keypointrcnn_resnet50_fpn'] = 'c' * 64
    elif operation == 'face.landmarks':
        params = dict(image=ref, faces=other, model_lock_id='c' * 64)
        result = dict(image=ref, faces=other, landmarks=[], model_id='fan2d', model_lock_id='c' * 64,
                      backend='mps', preprocessing_id='fan2d_crop256_v1', mean_confidence=0, warnings=[])
        policy['fan2d'] = 'c' * 64
    else:
        image = composition_fixture()[0]['source']['body']
        mask = dict(artifact=image['pixels'], canvas=image['canvas'], transform_chain=image['transform_chain'],
                    object_id=None, selector='person', confidence=1, model_lock_id=None, protected=False)
        params = dict(pair_id=str(uuid4()), before=ref, after=other, alignment=ref,
                      before_pose=ref, after_pose=other, before_person=mask, after_person=mask)
        result = dict(params=deepcopy(params), objects=[], warnings=[])
    if mode == 'source':
        if operation == 'pair.fit.body': result['params']['before'] = other
        else: result['image'] = other
    if mode == 'unknown_field': result['approved'] = True
    def pin(value): return dict(sha256=sha256(canonical_bytes(value)).hexdigest(), body=value)
    bundle = dict(schema_version=1, operation=operation, version='1', params=pin(params), result=pin(result))
    if mode == 'params_pin': bundle['params']['sha256'] = 'f' * 64
    if mode == 'result_pin': bundle['result']['sha256'] = 'f' * 64
    if mode == 'version': bundle['version'] = '2'
    if mode == 'boolean_schema': bundle['schema_version'] = True
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        assert validate_export_body_dependency_origin(raw, sha256(raw).hexdigest(), policy) is not None
    else:
        with pytest.raises(ValueError):
            validate_export_body_dependency_origin(raw, sha256(raw).hexdigest(), policy)


@pytest.mark.parametrize('mode', [
    'valid', 'after_pose_source', 'before_pose_source', 'after_mask_canvas',
    'before_mask_canvas', 'pose_model', 'person_model', 'alignment_source',
    'alignment_canvas', 'crop_aware', 'image_pin', 'pose_pin', 'fit_pin',
    'undeclared_faces', 'unknown_field', 'boolean_schema', 'accepted_unknown_pose',
])
def test_export_body_fit_full_paired_inputs(mode):
    from copy import deepcopy
    from uuid import uuid4
    from test_interchange_export_style import composition_fixture
    from photo_worker.v2.interchange_export_style import validate_export_body_fit_origin

    def pin(body):
        return dict(sha256=sha256(canonical_bytes(body)).hexdigest(), body=body)

    source = composition_fixture()[0]['source']['body']
    images = {side: pin(deepcopy(source)) for side in ('before', 'after')}
    refs = {side: dict(artifact_id=str(uuid4()), sha256=images[side]['sha256'])
            for side in images}
    poses, masks = {}, {}
    for side in images:
        pose = dict(image=deepcopy(refs[side]), poses=[],
                    model_id='keypointrcnn_resnet50_fpn', model_lock_id='c' * 64,
                    backend='mps', preprocessing_id='keypointrcnn_coco_v1',
                    detected_count=0, accepted_count=0, alpha_excluded_fraction=0, warnings=[])
        if mode == side + '_pose_source':
            pose['image']['artifact_id'] = str(uuid4())
        poses[side] = pin(pose)
        masks[side] = dict(artifact=deepcopy(source['pixels']), canvas=deepcopy(source['canvas']),
                           transform_chain=deepcopy(source['transform_chain']), object_id=None,
                           selector='person', confidence=1, model_lock_id='d' * 64, protected=False)
        if mode == side + '_mask_canvas': masks[side]['canvas']['width'] += 1
    alignment = dict(before=deepcopy(refs['before']), after=deepcopy(refs['after']),
                     before_canvas=deepcopy(source['canvas']), after_canvas=deepcopy(source['canvas']),
                     before_to_after=[1, 0, 0, 0, 1, 0, 0, 0, 1], overlap=1,
                     median_reprojection=None, inlier_count=0, confidence='user_confirmed',
                     crop_aware=False, warnings=[])
    if mode == 'alignment_source': alignment['after']['artifact_id'] = str(uuid4())
    if mode == 'alignment_canvas': alignment['after_canvas']['width'] += 1
    if mode == 'crop_aware': alignment['crop_aware'] = True
    alignment_pin = pin(alignment)
    params = dict(pair_id=str(uuid4()), before=refs['before'], after=refs['after'],
                  alignment=dict(artifact_id=str(uuid4()), sha256=alignment_pin['sha256']),
                  before_pose=dict(artifact_id=str(uuid4()), sha256=poses['before']['sha256']),
                  after_pose=dict(artifact_id=str(uuid4()), sha256=poses['after']['sha256']),
                  before_person=masks['before'], after_person=masks['after'])
    fit = dict(params=params, objects=[], warnings=[])
    if mode == 'accepted_unknown_pose':
        coefficients = dict(face_slim=0, waist_slim=0, hip_slim=0, torso_slim=0, leg_length=0)
        fit['objects'] = [dict(before_object_id=str(uuid4()), after_object_id=str(uuid4()),
                               confidence=1, match_cost=0, ambiguity_margin=1,
                               requested=coefficients, applied=deepcopy(coefficients),
                               metrics=dict(iou=1, landmark_error=0, silhouette_error=0,
                                            background_anchor_error=0), accepted=True, reason='applied')]
    bundle = dict(schema_version=1, fit=pin(fit), before=images['before'], after=images['after'],
                  alignment=alignment_pin, before_pose=poses['before'], after_pose=poses['after'],
                  before_faces=None, after_faces=None)
    if mode == 'image_pin': bundle['after']['sha256'] = 'f' * 64
    if mode == 'pose_pin': bundle['after_pose']['sha256'] = 'f' * 64
    if mode == 'fit_pin': bundle['fit']['sha256'] = 'f' * 64
    if mode == 'undeclared_faces': bundle['after_faces'] = pin({})
    if mode == 'unknown_field': bundle['approved'] = True
    if mode == 'boolean_schema': bundle['schema_version'] = True
    policy = {'keypointrcnn_resnet50_fpn': 'c' * 64, 'lraspp_mobilenet_v3_large': 'd' * 64}
    if mode == 'pose_model': policy['keypointrcnn_resnet50_fpn'] = 'e' * 64
    if mode == 'person_model': policy['lraspp_mobilenet_v3_large'] = 'e' * 64
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        result = validate_export_body_fit_origin(raw, sha256(raw).hexdigest(), policy)
        assert str(result.params.after.artifact_id) == refs['after']['artifact_id']
    else:
        with pytest.raises(ValueError):
            validate_export_body_fit_origin(raw, sha256(raw).hexdigest(), policy)


@pytest.mark.parametrize('mode', ['valid', 'source', 'confirmation', 'crop', 'params_pin', 'result_pin', 'operation', 'version', 'boolean_schema', 'unknown'])
def test_export_alignment_original_params(mode):
    from uuid import uuid4
    from photo_worker.v2.interchange_export_style import validate_export_pair_alignment_origin
    before = dict(artifact_id=str(uuid4()), sha256='a' * 64)
    after = dict(artifact_id=str(uuid4()), sha256='b' * 64)
    canvas = dict(width=32, height=32, orientation=1, space_id='working', source_sha256='c' * 64)
    params = dict(before=before, after=after)
    result = dict(before=before, after=after, before_canvas=canvas, after_canvas=canvas,
                  before_to_after=[1, 0, 0, 0, 1, 0, 0, 0, 1], overlap=1,
                  median_reprojection=0, inlier_count=12, confidence='measured', crop_aware=False, warnings=[])
    if mode == 'source': result['after'] = dict(after, artifact_id=str(uuid4()))
    if mode == 'confirmation': result.update(confidence='user_confirmed', median_reprojection=None, inlier_count=0)
    if mode == 'crop': result['crop_aware'] = True
    def pin(body): return dict(body=body, sha256=sha256(canonical_bytes(body)).hexdigest())
    bundle = dict(schema_version=1, operation='pair.register', version='1', params=pin(params), result=pin(result))
    if mode == 'params_pin': bundle['params']['sha256'] = 'f' * 64
    if mode == 'result_pin': bundle['result']['sha256'] = 'f' * 64
    if mode == 'operation': bundle['operation'] = 'pair.fit.body'
    if mode == 'version': bundle['version'] = '2'
    if mode == 'boolean_schema': bundle['schema_version'] = True
    if mode == 'unknown': bundle['approved'] = True
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        assert validate_export_pair_alignment_origin(raw, sha256(raw).hexdigest(), {}).confidence == 'measured'
    else:
        with pytest.raises(ValueError): validate_export_pair_alignment_origin(raw, sha256(raw).hexdigest(), {})


@pytest.mark.parametrize('mode', ['valid', 'source', 'model', 'preprocessing', 'count', 'params_pin', 'result_pin'])
def test_export_original_face_detector_binding(mode):
    from uuid import uuid4
    from photo_worker.v2.interchange_export_style import validate_export_body_dependency_origin
    image = dict(artifact_id=str(uuid4()), sha256='a' * 64)
    params = dict(image=image, model_lock_id='b' * 64)
    result = dict(image=image, faces=[], model_id='sfd', model_lock_id='b' * 64,
                  backend='mps', preprocessing_id='sfd_long1024_bgr_v1', preview_width=32,
                  preview_height=32, candidate_count=0, accepted_count=0,
                  alpha_excluded_fraction=0, warnings=[])
    if mode == 'source': result['image'] = dict(image, artifact_id=str(uuid4()))
    if mode == 'model': result['model_lock_id'] = 'c' * 64
    if mode == 'preprocessing': result['preprocessing_id'] = 'foreign'
    if mode == 'count': result['accepted_count'] = 1
    def pin(body): return dict(body=body, sha256=sha256(canonical_bytes(body)).hexdigest())
    bundle = dict(schema_version=1, operation='face.detect', version='1', params=pin(params), result=pin(result))
    if mode == 'params_pin': bundle['params']['sha256'] = 'f' * 64
    if mode == 'result_pin': bundle['result']['sha256'] = 'f' * 64
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        assert validate_export_body_dependency_origin(raw, sha256(raw).hexdigest(), {'sfd': 'b' * 64}).accepted_count == 0
    else:
        with pytest.raises(ValueError):
            validate_export_body_dependency_origin(raw, sha256(raw).hexdigest(), {'sfd': 'b' * 64})


@pytest.mark.parametrize('mode', ['valid', 'preview', 'identity', 'confidence', 'alpha', 'image_pin', 'model', 'unknown'])
def test_export_face_detector_actual_geometry(mode):
    from uuid import uuid4, uuid5, NAMESPACE_URL
    from test_interchange_export_style import composition_fixture
    from photo_worker.v2.interchange_export_style import validate_export_face_detector_origin
    from copy import deepcopy
    image = deepcopy(composition_fixture()[0]['source']['body'])
    image['alpha'] = None
    def pin(body): return dict(body=body, sha256=sha256(canonical_bytes(body)).hexdigest())
    image_pin = pin(image)
    ref = dict(artifact_id=str(uuid4()), sha256=image_pin['sha256'])
    params = dict(image=ref, model_lock_id='b' * 64)
    rect = dict(x0=.1, y0=.1, x1=.5, y1=.5)
    signature = ','.join(f'{rect[k]:.6f}' for k in ('x0', 'y0', 'x1', 'y1'))
    oid = str(uuid5(NAMESPACE_URL, f"face:{image['canvas']['source_sha256']}:{'b' * 64}:0:{signature}"))
    face = dict(object_id=oid, bbox_normalized=rect, confidence=.9)
    scale = min(1., 1024 / max(image['canvas']['width'], image['canvas']['height']))
    result = dict(image=ref, faces=[face], model_id='sfd', model_lock_id='b' * 64,
                  backend='mps', preprocessing_id='sfd_long1024_bgr_v1',
                  preview_width=max(1, round(image['canvas']['width'] * scale)),
                  preview_height=max(1, round(image['canvas']['height'] * scale)),
                  candidate_count=1, accepted_count=1, alpha_excluded_fraction=0, warnings=[])
    if mode == 'preview': result['preview_width'] = 1 if result['preview_width'] != 1 else 2
    if mode == 'identity': face['object_id'] = str(uuid4())
    if mode == 'confidence': face['confidence'] = .7
    if mode == 'alpha': result['alpha_excluded_fraction'] = .1
    bundle = dict(schema_version=1, params=pin(params), result=pin(result), image=image_pin)
    if mode == 'image_pin': bundle['image']['sha256'] = 'f' * 64
    if mode == 'unknown': bundle['approved'] = True
    policy = {'sfd': 'b' * 64}
    if mode == 'model': policy['sfd'] = 'c' * 64
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        assert validate_export_face_detector_origin(raw, sha256(raw).hexdigest(), policy).accepted_count == 1
    else:
        with pytest.raises(ValueError): validate_export_face_detector_origin(raw, sha256(raw).hexdigest(), policy)

@pytest.mark.parametrize('mode', ['valid', 'tile', 'strength', 'lock', 'extra', 'pin', 'version'])
def test_operation_params_denoise_preserves_controls_and_rejects_invalid_schema(mode):
    from uuid import uuid4
    from photo_worker.v2.interchange_export_style import validate_export_operation_params
    params = dict(image=dict(artifact_id=str(uuid4()), sha256='a' * 64),
                  strength=.75, model_id='scunet_color_real_psnr', model_lock_id='b' * 64,
                  color_adapter_id='work_srgb_delta_v1', tile=512, overlap=64)
    if mode == 'tile': params['tile'] = 1024
    if mode == 'strength': params['strength'] = 1.5
    if mode == 'lock': params['model_lock_id'] = 'unknown'
    if mode == 'extra': params['approved'] = True
    pin = sha256(canonical_bytes(params)).hexdigest()
    if mode == 'pin': pin = 'c' * 64
    bundle = dict(schema_version=1, operation='image.denoise', version='2' if mode == 'version' else '1',
                  params=dict(sha256=pin, body=params))
    raw = canonical_bytes(bundle)
    if mode == 'valid':
        typed = validate_export_operation_params(raw, sha256(raw).hexdigest(), {})
        assert canonical_bytes(typed) == canonical_bytes(params)
    else:
        with pytest.raises(ValueError):
            validate_export_operation_params(raw, sha256(raw).hexdigest(), {})

@pytest.mark.parametrize('scale,model', [(2, 'realesrgan_x2'), (4, 'realesrgan_x4'), (4, 'realesrgan_x2')])
def test_operation_params_upscale_requires_matching_scale_model(scale, model):
    from uuid import uuid4
    from photo_worker.v2.interchange_export_style import validate_export_operation_params
    params = dict(image=dict(artifact_id=str(uuid4()), sha256='a' * 64), scale=scale,
                  model_id=model, model_lock_id='b' * 64, color_adapter_id='work_srgb_delta_v1',
                  tile=256, overlap=32)
    bundle = dict(schema_version=1, operation='image.upscale', version='1',
                  params=dict(sha256=sha256(canonical_bytes(params)).hexdigest(), body=params))
    raw = canonical_bytes(bundle)
    if (scale, model) == (4, 'realesrgan_x2'):
        with pytest.raises(ValueError): validate_export_operation_params(raw, sha256(raw).hexdigest(), {})
    else:
        assert canonical_bytes(validate_export_operation_params(raw, sha256(raw).hexdigest(), {})) == canonical_bytes(params)
