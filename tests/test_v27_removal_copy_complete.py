"""Own removal copy path with physical NPY; object/admission doubles, no inference."""
from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4, uuid5
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.semantic.preview_removal_adopt_execute import execute_shown_removal_adoption
from test_v27_removal_preview_binding import removal_fixture
from test_v27_exposure_source import source_fixture
from test_v27_approved_semantic_edit_contract import fixture as approved_fixture
from test_v27_removal_preview_result_binding import presentation


def bundle(tmp_path, failure):
    params, candidate_raw, intent_raw = removal_fixture()
    edit, source, source_raw = source_fixture()
    source['alpha'] = dict(artifact_id=str(uuid4()), sha256='f'*64)
    source_raw = canonical_bytes(source)
    edit['source']['source']['sha256'] = sha256(source_raw).hexdigest()
    edit['source']['canvas_hash'] = canonical_hash(source['canvas'])
    params['source'] = edit['source']
    candidate = parse_json(candidate_raw); candidate['target'] = params['source']
    candidate_raw = canonical_bytes(candidate)
    params['candidate']['sha256'] = sha256(candidate_raw).hexdigest()
    documents = {}
    def pinned(value):
        raw = canonical_bytes(value)
        ref = dict(artifact_id=str(uuid4()), sha256=sha256(raw).hexdigest())
        documents[ref['artifact_id']] = raw
        return ref
    original = tmp_path/'shown.npy'
    values = np.full((1, 2, 3), .6, np.float32)
    np.save(original, values, allow_pickle=False)
    pixels = dict(artifact_id=str(uuid4()), sha256=sha256(original.read_bytes()).hexdigest())
    image = dict(source, pixels=pixels)
    if failure == 'color':image['color'] = dict(image['color'], icc_sha256='f'*64)
    image_ref = pinned(image)
    proof = dict(schema_version=1, params=params, working_image=image_ref, pixels=pixels,
        algorithm='big_lama_work_srgb_delta_v1', changed_pixels=1,
        protected_pixels_unchanged=True, outside_mask_unchanged=True,
        global_pass=False, roi=[0, 0, 2, 1])
    evidence_ref = pinned(proof)
    display = presentation()
    display.update(params=params, numerical_evidence=evidence_ref,
        working_image=image_ref, pixels=pixels, width=2, height=1)
    display_ref = pinned(display)
    intent = parse_json(intent_raw)
    approved = approved_fixture()
    approved.update(target_id=params['target_id'], source=params['source'],
        candidate=params['candidate'], intent=params['intent'], mask=candidate['mask'],
        transform=candidate['transform'], strength=intent['strength'], prompt=intent['prompt'])
    if failure == 'strength':approved['strength'] = .1 if intent['strength'] != .1 else .2
    approved_ref = pinned(approved)
    run = uuid4()
    adoption = dict(schema_version=1, tenant_id=params['tenant_id'], child_run_id=str(uuid4()),
        target_id=params['target_id'], approved_edit=approved_ref, preview_run_id=str(run),
        preview_task_id=str(uuid5(run, 'semantic.preview_remove_object:'+str(params['target_id']))),
        preview_result_id=str(uuid4()), preview_attempt=1, preview_fence=1,
        presentation=display_ref, numerical_evidence=evidence_ref, working_image=image_ref,
        pixels=pixels, model_lock_sha=params['model_lock_sha'],
        runtime_lock_sha=params['runtime_lock_sha'], device_profile_sha=params['device_profile_sha'])
    documents.update({params['candidate']['artifact_id']: candidate_raw,
        params['intent']['artifact_id']: intent_raw, params['source']['source']['artifact_id']: source_raw})
    return adoption, documents, original, image


@pytest.mark.parametrize('failure', [None, 'color', 'strength', 'source_bytes',
    'pixel_sha', 'image_sha', 'image_mutation', 'late_revoke', 'pixel_input_alias', 'image_input_alias',
    'pixel_alpha_alias', 'image_alpha_alias'])
def test_complete_removal_copy_preserves_exact_shown_bytes(tmp_path, failure):
    params, documents, original, image = bundle(tmp_path, failure)
    if failure == 'source_bytes':original.write_bytes(original.read_bytes()+b'extra')
    calls = []; allowed = True
    class Artifacts:
        def contract_bytes(self, ref, model):
            return documents[str(ref.artifact_id)]
        @contextmanager
        def input(self, ref, **kwargs):
            assert ref == ArtifactRef.model_validate_json(canonical_bytes(params['pixels']))
            yield SimpleNamespace(path=original)
        def stage(self, path, reservation, role, media):
            calls.append(role)
            assert path.read_bytes() == original.read_bytes()
            return ArtifactRef(artifact_id=params['approved_edit']['artifact_id'] if failure == 'pixel_input_alias' else image['alpha']['artifact_id'] if failure == 'pixel_alpha_alias' else uuid4(), sha256='0'*64 if failure == 'pixel_sha' else sha256(path.read_bytes()).hexdigest())
        def stage_contract(self, value, role):
            nonlocal allowed
            calls.append(role)
            expected = WorkingImage.model_validate_json(canonical_bytes(image))
            assert value.model_copy(update={'pixels': expected.pixels}) == expected
            digest = sha256(canonical_bytes(value)).hexdigest()
            if failure == 'image_mutation':value.color.icc_sha256 = '0'*64
            if failure == 'late_revoke':allowed = False
            return ArtifactRef(artifact_id=params['approved_edit']['artifact_id'] if failure == 'image_input_alias' else image['alpha']['artifact_id'] if failure == 'image_alpha_alias' else uuid4(), sha256='0'*64 if failure == 'image_sha' else digest)
    artifacts = Artifacts()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2*1048576, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        def execute():
            return execute_shown_removal_adoption(artifacts, params,
                verify_admission=lambda _: allowed, verify_runtime=lambda: True,
                verify_color=lambda _: True, check_active=lambda: None,
                max_output_bytes=2*1048576, expected_pixel_count=2)
        if failure:
            with pytest.raises(ValueError):execute()
            if failure in ('color', 'strength', 'source_bytes'):assert calls == []
        else:
            result = execute()
            assert calls == ['pixels', 'working_image']
            assert result.pixels.sha256 == params['pixels']['sha256']
            assert result.bytes_written > original.stat().st_size
