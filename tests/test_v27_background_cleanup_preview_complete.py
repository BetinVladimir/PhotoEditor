import pytest
from photo_worker.operations.semantic.preview_background_cleanup_complete import execute_complete_background_cleanup_preview
from test_v27_background_cleanup_preview import background_fixture as fixture
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('field', ['ram', 'output'])
def test_complete_preview_rejects_budget_before_compute(tmp_path, monkeypatch, field):
    params, _, _ = fixture()
    edit, _, raw = source_fixture()
    params['source'] = edit['source']
    class Artifacts:
        def contract_bytes(self, ref, model):
            return raw
    def forbidden(*args, **kwargs):
        raise AssertionError('compute reached before full presentation budget')
    monkeypatch.setattr('photo_worker.operations.semantic.preview_background_cleanup_complete.execute_artifact_background_cleanup_preview', forbidden)
    with pytest.raises(ValueError, match='SEMANTIC_REMOVAL_RESOURCE_PROFILE'):
        execute_complete_background_cleanup_preview(Artifacts(), params, object(), icc_root=tmp_path,
            icc_lock_sha='a'*64, srgb_sha='b'*64,
            max_bytes=1 if field == 'ram' else 2**40,
            max_output_bytes=1 if field == 'output' else 2**40,
            verify_admission=lambda _: True, verify_origin=lambda *a: True,
            verify_color=lambda *a: True, verify_runtime=lambda: True, check_active=lambda: None)


@pytest.mark.parametrize('failure',[None,'after_upload','after_sha','after_alias','evidence_sha','evidence_alias','final_revocation'])
def test_complete_bundle_real_arrays_and_staging(tmp_path,monkeypatch,failure):
    """Exercise orchestration/NPY/PNG/linkage; ICC conversion is a pinned test double.

    This is not a model or color qualification fixture.
    """
    from hashlib import sha256
    from types import SimpleNamespace
    from uuid import uuid4
    import numpy as np
    from PIL import Image
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
    from photo_worker.contracts.semantic_preview import BackgroundCleanupPreviewPresentation,BackgroundCleanupPreviewNumericalEvidence
    from photo_worker.operations.semantic.preview_presentation import ExposureDisplay
    from photo_worker.operations.semantic import preview_background_cleanup_media
    from photo_worker.runtime.scratch import ScratchSpace
    from test_v27_preview_loader import setup
    from test_v27_background_cleanup_preview import Engine
    params,source,events=setup(tmp_path)
    intent_id=params['intent']['artifact_id']
    intent=parse_json(source.contract_bytes(SimpleNamespace(artifact_id=intent_id),None))
    intent['kind']='background_cleanup';intent['ev']=None
    intent_raw=canonical_bytes(intent)
    params['intent']['sha256']=sha256(intent_raw).hexdigest()
    params.update(kind='background_cleanup',model_id='big_lama',context_pixels=64,feather_pixels=8)
    profile=b'bundle-unit-pinned-profile';srgb_sha=sha256(profile).hexdigest()
    def display(image,rgb,alpha,**kwargs):
        assert kwargs['srgb_sha']==srgb_sha
        return ExposureDisplay(rgb.copy(),None if alpha is None else alpha.copy(),profile,srgb_sha)
    monkeypatch.setattr(preview_background_cleanup_media,'prepare_exposure_display',display)
    documents={};binaries={};refs={};roles=[];revoked=False
    class Artifacts:
        input=source.input
        def contract_bytes(self,ref,model):
            if str(ref.artifact_id)==intent_id:return intent_raw
            return source.contract_bytes(ref,model)
        def stage(self,path,reservation,role,media):
            roles.append(role)
            if failure=='after_upload' and role=='after':raise RuntimeError('sidecar upload failed')
            raw=path.read_bytes()
            ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
            if failure=='after_sha' and role=='after':ref=ref.model_copy(update={'sha256':'0'*64})
            if failure=='after_alias' and role=='after':ref=refs['before']
            refs[role]=ref;binaries[role]=raw
            if media=='image/png':
                with Image.open(path) as png:
                    assert png.size==(8,8)
                    if role in ('before','after'):assert png.info=={'icc_profile':profile}
            return ref
        def stage_contract(self,value,role):
            nonlocal revoked
            roles.append(role);raw=canonical_bytes(value);documents[role]=raw
            ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest());refs[role]=ref
            if failure=='evidence_sha' and role=='preview_evidence':ref=ref.model_copy(update={'sha256':'0'*64})
            if failure=='evidence_alias' and role=='preview_evidence':ref=refs['working_image']
            if failure=='final_revocation' and role=='preview_presentation':revoked=True
            return ref
    artifacts=Artifacts();engine=Engine()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,32*1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        def execute():
            return execute_complete_background_cleanup_preview(artifacts,params,engine,icc_root=tmp_path,
                icc_lock_sha='a'*64,srgb_sha=srgb_sha,max_bytes=2**30,max_output_bytes=32*1048576,
                verify_admission=lambda _:not revoked,verify_origin=lambda *a:True,
                verify_color=lambda _:True,verify_runtime=lambda:True,check_active=lambda:None)
        if failure:
            with pytest.raises((RuntimeError,ValueError)):execute()
            if failure!='final_revocation':assert 'preview_presentation' not in roles
        else:
            result=execute()
            evidence=BackgroundCleanupPreviewNumericalEvidence.model_validate_json(documents['preview_evidence'])
            presentation=BackgroundCleanupPreviewPresentation.model_validate_json(documents['preview_presentation'])
            assert roles==['pixels','working_image','preview_evidence','before','after','affected','protected','preview_presentation']
            assert engine.calls==1 and evidence.changed_pixels>0
            assert evidence.working_image==presentation.working_image==refs['working_image']
            assert evidence.pixels==presentation.pixels==refs['pixels']
            assert result.presentation==refs['preview_presentation']
            assert result.evidence==presentation.numerical_evidence==refs['preview_evidence']
            assert result.bytes_written==sum(map(len,documents.values()))+sum(map(len,binaries.values()))
            assert len({ref.artifact_id for ref in refs.values()})==8
    assert events.count('open')==events.count('close')


@pytest.mark.parametrize('boundary', ['WorkingImage', 'TransferCandidate', 'EditIntent'])
@pytest.mark.parametrize('loss', ['cancel', 'admission', 'runtime'])
def test_complete_preview_stops_after_authority_loss_during_document_read(tmp_path, monkeypatch, boundary, loss):
    """An in-flight read must not permit later reads, compute or media staging."""
    from types import SimpleNamespace
    from photo_worker.operations.semantic import preview_background_cleanup_complete as module
    params, _, _ = fixture()
    edit, _, raw = source_fixture()
    params['source'] = edit['source']
    lost = False
    reads = []
    downstream = []

    class Artifacts:
        def contract_bytes(self, ref, model):
            nonlocal lost
            reads.append(model.__name__)
            if model.__name__ == boundary:
                lost = True
            return raw

    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('preview cancelled during document read')

    def compute(*args, **kwargs):
        downstream.append('compute')
        return SimpleNamespace(candidate=b'unused', preservation=SimpleNamespace(changed_pixels=0),
            roi=(0, 0, 1, 1), global_pass=False)

    def forbidden(*args, **kwargs):
        raise AssertionError('media preparation reached after authority loss')

    monkeypatch.setattr(module, 'execute_artifact_background_cleanup_preview', compute)
    monkeypatch.setattr(module, 'load_background_cleanup_preview_arrays', lambda *a, **k: object())
    monkeypatch.setattr(module, 'load_background_cleanup_preview_alpha', lambda *a, **k: None)
    monkeypatch.setattr(module, 'prepare_background_cleanup_preview_media', forbidden)
    error = InterruptedError if loss == 'cancel' else ValueError
    with pytest.raises(error):
        execute_complete_background_cleanup_preview(Artifacts(), params, object(), icc_root=tmp_path,
            icc_lock_sha='a'*64, srgb_sha='b'*64, max_bytes=2**40, max_output_bytes=2**40,
            verify_admission=lambda _: not (lost and loss == 'admission'),
            verify_origin=lambda *a: True, verify_color=lambda *a: True,
            verify_runtime=lambda: not (lost and loss == 'runtime'), check_active=active)
    expected = ['WorkingImage', 'TransferCandidate', 'EditIntent']
    assert reads == expected[:expected.index(boundary)+1]
    assert downstream == ([] if boundary == 'WorkingImage' else ['compute'])


@pytest.mark.parametrize('boundary', ['compute', 'arrays', 'alpha', 'media'])
@pytest.mark.parametrize('loss', ['cancel', 'admission', 'runtime'])
def test_complete_preview_fences_each_expensive_phase(tmp_path, monkeypatch, boundary, loss):
    from types import SimpleNamespace
    from photo_worker.operations.semantic import preview_background_cleanup_complete as module
    params, _, _ = fixture()
    edit, _, raw = source_fixture()
    params['source'] = edit['source']
    phases = []
    reads = []
    lost = False
    def mark(phase):
        nonlocal lost
        phases.append(phase)
        if phase == boundary:
            lost = True
    class Artifacts:
        def contract_bytes(self, ref, model):
            reads.append(model.__name__)
            return raw
    def compute(*a, **k):
        mark('compute')
        return SimpleNamespace(candidate=b'not-published',
            preservation=SimpleNamespace(changed_pixels=0), roi=(0,0,1,1), global_pass=False)
    def arrays(*a, **k):
        mark('arrays')
        return object()
    def alpha(*a, **k):
        mark('alpha')
        return None
    def media(*a, **k):
        mark('media')
        return object()
    def forbidden(*a, **k):
        raise AssertionError('staging reached after phase authority loss')
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during preview phase')
    monkeypatch.setattr(module, 'execute_artifact_background_cleanup_preview', compute)
    monkeypatch.setattr(module, 'load_background_cleanup_preview_arrays', arrays)
    monkeypatch.setattr(module, 'load_background_cleanup_preview_alpha', alpha)
    monkeypatch.setattr(module, 'prepare_background_cleanup_preview_media', media)
    monkeypatch.setattr(module, 'stage_background_cleanup_preview', forbidden)
    with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
        execute_complete_background_cleanup_preview(Artifacts(), params, object(), icc_root=tmp_path,
            icc_lock_sha='a'*64, srgb_sha='b'*64, max_bytes=2**40, max_output_bytes=2**40,
            verify_admission=lambda _: not (lost and loss == 'admission'),
            verify_origin=lambda *a: True, verify_color=lambda *a: True,
            verify_runtime=lambda: not (lost and loss == 'runtime'), check_active=active)
    expected = ['compute', 'arrays', 'alpha', 'media']
    assert phases == expected[:expected.index(boundary)+1]
    assert reads == (['WorkingImage', 'TransferCandidate', 'EditIntent']
        if boundary == 'media' else ['WorkingImage'])
