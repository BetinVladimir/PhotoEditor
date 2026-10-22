import pytest
from photo_worker.operations.semantic.preview_removal_complete import execute_complete_removal_preview
from test_v27_removal_preview_binding import removal_fixture as fixture
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
    monkeypatch.setattr('photo_worker.operations.semantic.preview_removal_complete.execute_artifact_removal_preview', forbidden)
    with pytest.raises(ValueError, match='SEMANTIC_REMOVAL_RESOURCE_PROFILE'):
        execute_complete_removal_preview(Artifacts(), params, object(), icc_root=tmp_path,
            icc_lock_sha='a'*64, srgb_sha='b'*64,
            max_bytes=1 if field == 'ram' else 2**40,
            max_output_bytes=1 if field == 'output' else 2**40,
            verify_admission=lambda _: True, verify_origin=lambda *a: True,
            verify_color=lambda *a: True, verify_runtime=lambda: True, check_active=lambda: None)


@pytest.mark.parametrize('failure',[None,'after_upload','after_sha','after_alias','final_revocation',
    'false_protected_proof','false_outside_proof','missing_declared_pixel',
    'declared_pixel_sha','changed_upload_file','evidence_mutation'])
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
    from photo_worker.contracts.semantic_preview import RemovalPreviewPresentation,RemovalPreviewNumericalEvidence
    from photo_worker.operations.semantic.preview_presentation import ExposureDisplay
    from photo_worker.operations.semantic import preview_removal_media
    from photo_worker.runtime.scratch import ScratchSpace
    from test_v27_preview_loader import setup
    from test_v27_removal_preview import Engine
    params,source,events=setup(tmp_path)
    intent_id=params['intent']['artifact_id']
    intent=parse_json(source.contract_bytes(SimpleNamespace(artifact_id=intent_id),None))
    intent['kind']='remove_object';intent['ev']=None
    intent_raw=canonical_bytes(intent)
    params['intent']['sha256']=sha256(intent_raw).hexdigest()
    params.update(kind='remove_object',model_id='big_lama',context_pixels=64,feather_pixels=8)
    profile=b'bundle-unit-pinned-profile';srgb_sha=sha256(profile).hexdigest()
    def display(image,rgb,alpha,**kwargs):
        assert kwargs['srgb_sha']==srgb_sha
        return ExposureDisplay(rgb.copy(),None if alpha is None else alpha.copy(),profile,srgb_sha)
    monkeypatch.setattr(preview_removal_media,'prepare_exposure_display',display)
    documents={};binaries={};refs={};roles=[];revoked=False
    class Artifacts:
        input=source.input
        def declared_input_refs(self, *, maximum):
            image = parse_json(source.contract_bytes(SimpleNamespace(
                artifact_id=params['source']['source']['artifact_id']), None))
            values = [params[key] for key in ('plan', 'intent', 'candidate', 'protection')]
            values += [params['source']['source'], image['pixels']]
            if image['alpha'] is not None:
                values.append(image['alpha'])
            if failure == 'missing_declared_pixel':
                values.pop(5)
            if failure == 'declared_pixel_sha':
                values[5] = dict(values[5], sha256='0'*64)
            return tuple(ArtifactRef.model_validate_json(canonical_bytes(value)) for value in values)
        def contract_bytes(self,ref,model):
            if str(ref.artifact_id)==intent_id:return intent_raw
            return source.contract_bytes(ref,model)
        def stage(self,path,reservation,role,media):
            roles.append(role)
            if failure=='after_upload' and role=='after':raise RuntimeError('sidecar upload failed')
            raw=path.read_bytes()
            if failure == 'changed_upload_file' and role == 'pixels':
                path.write_bytes(raw+b'changed-after-upload')
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
            if failure == 'evidence_mutation' and role == 'preview_evidence':
                value.changed_pixels = 0
            ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest());refs[role]=ref
            if failure=='final_revocation' and role=='preview_presentation':revoked=True
            return ref
    if failure in ('false_protected_proof', 'false_outside_proof'):
        from dataclasses import replace
        from photo_worker.operations.semantic import preview_removal_complete as module
        compute = module.execute_artifact_removal_preview
        def altered_proof(*args, **kwargs):
            result = compute(*args, **kwargs)
            field = 'protected_unchanged' if failure == 'false_protected_proof' else 'outside_mask_unchanged'
            return replace(result, preservation=replace(result.preservation, **{field: False}))
        monkeypatch.setattr(module, 'execute_artifact_removal_preview', altered_proof)
    artifacts=Artifacts();engine=Engine()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,32*1048576,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        def execute():
            return execute_complete_removal_preview(artifacts,params,engine,icc_root=tmp_path,
                icc_lock_sha='a'*64,srgb_sha=srgb_sha,max_bytes=2**30,max_output_bytes=32*1048576,
                verify_admission=lambda _:not revoked,verify_origin=lambda *a:True,
                verify_color=lambda _:True,verify_runtime=lambda:True,check_active=lambda:None)
        if failure:
            with pytest.raises((RuntimeError,ValueError)):execute()
            if failure!='final_revocation':assert 'preview_presentation' not in roles
        else:
            result=execute()
            evidence=RemovalPreviewNumericalEvidence.model_validate_json(documents['preview_evidence'])
            presentation=RemovalPreviewPresentation.model_validate_json(documents['preview_presentation'])
            assert roles==['pixels','working_image','preview_evidence','before','after','affected','protected','preview_presentation']
            assert engine.calls==1 and evidence.changed_pixels>0
            assert evidence.protected_pixels_unchanged is True
            assert evidence.outside_mask_unchanged is True
            assert evidence.working_image==presentation.working_image==refs['working_image']
            assert evidence.pixels==presentation.pixels==refs['pixels']
            assert result.presentation==refs['preview_presentation']
            assert result.evidence==presentation.numerical_evidence==refs['preview_evidence']
            assert result.bytes_written==sum(map(len,documents.values()))+sum(map(len,binaries.values()))
            assert len({ref.artifact_id for ref in refs.values()})==8
    assert events.count('open')==events.count('close')


@pytest.mark.parametrize('loss',['cancel','admission','runtime'])
def test_complete_source_read_loss_stops_before_binding_or_compute(tmp_path,monkeypatch,loss):
    from photo_worker.operations.semantic import preview_removal_complete as module
    from photo_worker.runtime.executor import TaskCancelled
    params,_,_=fixture();revoked=False;reads=[]
    class Artifacts:
        def contract_bytes(self,ref,model):
            nonlocal revoked
            reads.append(model.__name__);revoked=True
            return b'not-json'
    def active():
        if revoked and loss=='cancel':raise TaskCancelled('cancelled during source read')
    def forbidden(*args,**kwargs):raise AssertionError('lost authority reached binding/compute')
    monkeypatch.setattr(module,'bind_removal_preview_source',forbidden)
    monkeypatch.setattr(module,'execute_artifact_removal_preview',forbidden)
    with pytest.raises(TaskCancelled if loss=='cancel' else ValueError):
        execute_complete_removal_preview(Artifacts(),params,object(),icc_root=tmp_path,
            icc_lock_sha='a'*64,srgb_sha='b'*64,max_bytes=2**40,max_output_bytes=2**40,
            verify_admission=lambda _:not(revoked and loss=='admission'),
            verify_runtime=lambda:not(revoked and loss=='runtime'),verify_origin=lambda *args:True,
            verify_color=lambda *args:True,check_active=active)
    assert reads==['WorkingImage']
