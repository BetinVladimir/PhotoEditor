from photo_worker.contracts.base import canonical_bytes
import pytest
from photo_worker.contracts.semantic_preview import ShownBackgroundCleanupAdoptionInput
from photo_worker.operations.semantic.preview_background_cleanup_adopt_execute import execute_shown_background_cleanup_adoption
from test_v27_preview_adoption_contract import fixture as exposure_fixture
from uuid import UUID,uuid5


def fixture():
    value=exposure_fixture()
    value['preview_task_id']=str(uuid5(UUID(str(value['preview_run_id'])),'semantic.preview_background_cleanup:'+str(value['target_id'])))
    # This fixture represents the JSON Params document, not Python-native UUID input.
    import json
    return json.loads(json.dumps(value, default=str))


@pytest.mark.parametrize('accepted',[False,None,1])
def test_adoption_requires_explicit_live_authority_before_documents(accepted):
    class Forbidden:
        def contract_bytes(self,*args):raise AssertionError('unadmitted document IO')
    with pytest.raises(ValueError,match='SEMANTIC_PREVIEW_ADOPTION_DENIED'):
        execute_shown_background_cleanup_adoption(Forbidden(),ShownBackgroundCleanupAdoptionInput.model_validate_json(canonical_bytes(fixture())),
            verify_admission=lambda _:accepted,verify_runtime=lambda:True,
            verify_color=lambda _:True,check_active=lambda:None,max_output_bytes=2**40,expected_pixel_count=1)


def copy_documents_fixture(tmp_path):
    """Own hash-bound cleanup documents with real NPY; no storage authority."""
    from hashlib import sha256
    from uuid import uuid4
    import numpy as np
    from photo_worker.contracts.base import canonical_bytes,parse_json
    from photo_worker.contracts.semantic_preview import BackgroundCleanupPreviewPresentation,BackgroundCleanupPreviewNumericalEvidence
    from test_v27_exposure_geometry import geometry_fixture
    from test_v27_background_cleanup_preview import background_fixture
    edit,candidate_raw,intent_raw,source_raw,*_ = geometry_fixture()
    source=parse_json(source_raw)
    path=tmp_path/'selected.npy'
    np.save(path,np.full((8,8,3),.5,dtype=np.float32),allow_pickle=False)
    source['pixels']=dict(artifact_id=str(uuid4()),sha256=sha256(path.read_bytes()).hexdigest())
    source_raw=canonical_bytes(source)
    edit['source']['source']['sha256']=sha256(source_raw).hexdigest()
    candidate=parse_json(candidate_raw);candidate['target']=edit['source']
    candidate_raw=canonical_bytes(candidate);edit['candidate']['sha256']=sha256(candidate_raw).hexdigest()
    intent=parse_json(intent_raw);intent.update(kind='background_cleanup',ev=None,reference=edit['source'])
    intent_raw=canonical_bytes(intent);edit['intent']['sha256']=sha256(intent_raw).hexdigest()
    edit.update(kind='background_cleanup',ev=None)
    params,_,_=background_fixture()
    params.update(source=edit['source'],candidate=edit['candidate'],intent=edit['intent'],target_id=edit['target_id'],snapshot_id=intent['target_snapshot_id'])
    def ref(raw):return dict(artifact_id=str(uuid4()),sha256=sha256(raw).hexdigest())
    # Shown image can reuse source pixel bytes but has its own document identity.
    image_ref=ref(source_raw)
    evidence=dict(schema_version=1,params=params,working_image=image_ref,pixels=source['pixels'],algorithm='big_lama_work_srgb_delta_v1',
        changed_pixels=0,protected_pixels_unchanged=True,outside_mask_unchanged=True,global_pass=True,roi=[0,0,8,8])
    evidence_raw=canonical_bytes(BackgroundCleanupPreviewNumericalEvidence.model_validate_json(canonical_bytes(evidence)));evidence_ref=ref(evidence_raw)
    presentation=dict(schema_version=1,params=params,working_image=image_ref,pixels=source['pixels'],numerical_evidence=evidence_ref,
        **{role:ref(role.encode()) for role in ('before','after','affected','protected')},width=8,height=8,
        icc_lock_sha='b'*64,srgb_sha='c'*64,resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1')
    presentation_raw=canonical_bytes(BackgroundCleanupPreviewPresentation.model_validate_json(canonical_bytes(presentation)));presentation_ref=ref(presentation_raw)
    approved_raw=canonical_bytes(edit);approved_ref=ref(approved_raw)
    copy=fixture()
    copy.update(tenant_id=params['tenant_id'],target_id=params['target_id'],approved_edit=approved_ref,
        presentation=presentation_ref,numerical_evidence=evidence_ref,working_image=image_ref,pixels=source['pixels'],
        **{k:params[k] for k in ('model_lock_sha','runtime_lock_sha','device_profile_sha')})
    copy['preview_task_id']=str(uuid5(UUID(str(copy['preview_run_id'])),'semantic.preview_background_cleanup:'+str(copy['target_id'])))
    owned=ShownBackgroundCleanupAdoptionInput.model_validate_json(canonical_bytes(copy))
    documents={r['artifact_id']:raw for r,raw in [(approved_ref,approved_raw),(presentation_ref,presentation_raw),
        (evidence_ref,evidence_raw),(image_ref,source_raw),(params['candidate'],candidate_raw),
        (params['intent'],intent_raw),(params['source']['source'],source_raw)]}
    return owned,documents,path


def test_background_cleanup_full_document_binding_owns_exact_shown_image(tmp_path):
    from photo_worker.operations.semantic.preview_background_cleanup_adopt_binding import bind_shown_background_cleanup_adoption
    params,documents,path=copy_documents_fixture(tmp_path)
    from photo_worker.contracts.base import parse_json
    preview=parse_json(documents[str(params.presentation.artifact_id)])['params']
    raws=[documents[str(r.artifact_id)] for r in (params.approved_edit,params.presentation,params.numerical_evidence,params.working_image)]
    raws += [documents[preview[key]['artifact_id']] for key in ('candidate','intent')]
    raws += [documents[preview['source']['source']['artifact_id']]]
    _,edit,_,image=bind_shown_background_cleanup_adoption(params,*raws)
    assert edit.kind=='background_cleanup' and image.pixels==params.pixels and path.is_file()
    for index in range(len(raws)):
        changed=list(raws);changed[index]+=b' '
        with pytest.raises(ValueError):bind_shown_background_cleanup_adoption(params,*changed)


@pytest.mark.parametrize('failure',[None,'revoked_after_pixels','pixel_count','color',*[f'document_{i}' for i in range(1,8)]])
def test_background_cleanup_execute_copies_real_shown_npy(tmp_path,failure):
    from contextlib import contextmanager
    from hashlib import sha256
    from types import SimpleNamespace
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    params,documents,source=copy_documents_fixture(tmp_path)
    source_bytes=source.read_bytes()
    stages=[];reads=[];closed=[];authority=True
    class Scratch:
        @contextmanager
        def reserve(self,size):
            assert size==len(source_bytes)
            yield SimpleNamespace(path=lambda *args:tmp_path/'copied.npy',validate_file=lambda path:None)
    class Artifacts:
        scratch=None
        def contract_bytes(self,ref,model):
            nonlocal authority
            assert authority,'document IO after revocation'
            raw=documents[str(ref.artifact_id)]
            assert sha256(raw).hexdigest()==ref.sha256
            model.model_validate_json(raw)
            reads.append(model.__name__)
            if failure==f'document_{len(reads)}':authority=False
            return raw
        @contextmanager
        def input(self,ref,**kwargs):
            assert ref==params.pixels
            try:yield SimpleNamespace(path=source)
            finally:closed.append(True)
        def stage(self,path,reservation,role,media):
            nonlocal authority
            assert role=='pixels' and media=='application/x-npy' and path.read_bytes()==source_bytes
            stages.append(role)
            if failure=='revoked_after_pixels':authority=False
            return ArtifactRef(artifact_id=uuid4(),sha256=sha256(source_bytes).hexdigest())
        def stage_contract(self,image,role):
            assert authority and role=='working_image'
            stages.append(role)
            assert image.pixels.sha256==params.pixels.sha256
            return ArtifactRef(artifact_id=uuid4(),sha256=sha256(canonical_bytes(image)).hexdigest())
    def active():
        if not authority:raise ValueError('revoked')
    def run():
        from photo_worker.runtime.scratch import ScratchSpace
        with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2**20, min_free_bytes=0) as scratch:
            artifacts=Artifacts();artifacts.scratch=scratch
            return execute_shown_background_cleanup_adoption(artifacts,params,
                verify_admission=lambda value:authority and value==params,verify_runtime=lambda:True,
                verify_color=lambda color:failure!='color',check_active=active,
                max_output_bytes=2**40,expected_pixel_count=65 if failure=='pixel_count' else 64)
    if failure is None:
        result=run()
        assert result.pixels.sha256==params.pixels.sha256 and result.bytes_written>len(source_bytes)
        assert stages==['pixels','working_image'] and closed==[True]
    else:
        with pytest.raises(ValueError):run()
        assert stages==(['pixels'] if failure=='revoked_after_pixels' else [])
        assert closed==([True] if failure=='revoked_after_pixels' else [])
    expected_reads=['ApprovedSemanticEdit','BackgroundCleanupPreviewPresentation','BackgroundCleanupPreviewNumericalEvidence',
        'WorkingImage','TransferCandidate','EditIntent','WorkingImage']
    if isinstance(failure,str) and failure.startswith('document_'):
        expected_reads=expected_reads[:int(failure.split('_')[1])]
    assert reads==expected_reads


@pytest.mark.parametrize('read_number', range(1,8))
@pytest.mark.parametrize('loss', ['cancel','admission','runtime'])
def test_shown_cleanup_copy_fences_each_bound_document_read(tmp_path, monkeypatch, read_number, loss):
    params, documents, _ = copy_documents_fixture(tmp_path)
    reads = []
    lost = False
    class Artifacts:
        def contract_bytes(self, ref, model):
            nonlocal lost
            reads.append(ref.artifact_id)
            raw = documents[str(ref.artifact_id)]
            if len(reads) == read_number:
                lost = True
            return raw
    def active():
        if lost and loss == 'cancel':
            raise InterruptedError('cancelled during shown cleanup read')
    def forbidden(*a, **k):
        raise AssertionError('pixel copy reached after authority loss')
    monkeypatch.setattr('photo_worker.operations.semantic.preview_background_cleanup_adopt_execute.stage_shown_preview_image', forbidden)
    with pytest.raises(InterruptedError if loss == 'cancel' else ValueError):
        execute_shown_background_cleanup_adoption(Artifacts(), params,
            verify_admission=lambda _: not (lost and loss == 'admission'),
            verify_runtime=lambda: not (lost and loss == 'runtime'),
            verify_color=lambda _: True, check_active=active,
            max_output_bytes=2**30, expected_pixel_count=64)
    assert len(reads) == read_number
