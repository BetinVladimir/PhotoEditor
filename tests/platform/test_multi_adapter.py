from types import SimpleNamespace
import pytest
from test_multi_admission import installed
from photo_worker.operations.optical.multi_adapter import execute
from photo_worker.operations.optical.multi_source_closure import verify_multi_source_closure
from photo_worker.runtime.executor import OperationFailure


def test_missing_admission_fails_before_any_artifact_read():
    class NoIO:
        def params(self,*args):raise AssertionError('unexpected IO')
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend':'cpu','renderer_version':'v2.2'}))
    with pytest.raises(OperationFailure,match='QUALIFICATION_UNAVAILABLE'):
        execute(ctx,NoIO(),None,operation='optical.panorama')


def artifacts_for(params,images,*,omit_child=False):
    class Artifacts:
        def params(self,model):return params
        def contract(self,ref,model):return images[params.frames.index(next(b for b in params.frames if b.source==ref))]
        def declared_contract_refs(self,contract,maximum):
            if contract=='WorkingImage':return tuple(b.source for b in params.frames)
            if contract=='NpyArray':return () if omit_child else tuple(image.pixels for image in images)
            return ()
        def input(self,*args,**kwargs):raise AssertionError('pixel IO must follow admission')
    return Artifacts()


def test_refused_admission_precedes_pixel_io(monkeypatch):
    admission,ctx,params,images,_,_,_=installed(monkeypatch)
    ctx.lease.execution_profile.update(backend='cpu',renderer_version='v2.2')
    def refuse(*args):raise OperationFailure('MULTI_QUALIFICATION_UNAVAILABLE')
    admission.admit=refuse
    with pytest.raises(OperationFailure,match='QUALIFICATION_UNAVAILABLE'):
        execute(ctx,artifacts_for(params,images),admission,operation='optical.panorama')


def test_undeclared_pixel_child_rejected_before_admission(monkeypatch):
    admission,ctx,params,images,_,calls,_=installed(monkeypatch)
    ctx.lease.execution_profile.update(backend='cpu',renderer_version='v2.2')
    with pytest.raises(OperationFailure,match='CHILD_UNDECLARED'):
        execute(ctx,artifacts_for(params,images,omit_child=True),admission,operation='optical.panorama')
    assert calls==[]


def test_reordered_sources_cannot_pass_closure(monkeypatch):
    _,_,params,images,_,_,_=installed(monkeypatch)
    with pytest.raises(ValueError,match='SOURCE_MISMATCH'):
        verify_multi_source_closure(artifacts_for(params,images),params,list(reversed(images)))


def test_cross_source_document_array_alias_refused_before_pixel_io(monkeypatch):
    from photo_worker.contracts.base import canonical_hash
    _,_,params,images,_,_,_=installed(monkeypatch)
    images[0].pixels.artifact_id=params.frames[1].source.artifact_id
    params.frames[0].source.sha256=canonical_hash(images[0])
    with pytest.raises(ValueError,match='MULTI_SOURCE_CHILD_ALIAS'):
        verify_multi_source_closure(artifacts_for(params,images),params,images)


@pytest.mark.parametrize('conflict',['different_sha','document_array'])
def test_declared_multi_closure_refuses_conflicting_artifact_identity(monkeypatch,conflict):
    from photo_worker.contracts.base import ArtifactRef
    _,_,params,images,_,_,_=installed(monkeypatch)
    artifacts=artifacts_for(params,images)
    declared=artifacts.declared_contract_refs
    def conflicting(contract,maximum):
        refs=declared(contract,maximum)
        if contract!='NpyArray':return refs
        source=images[0].pixels if conflict=='different_sha' else params.frames[0].source
        return refs+(ArtifactRef(artifact_id=source.artifact_id,
            sha256='f'*64 if conflict=='different_sha' else source.sha256),)
    artifacts.declared_contract_refs=conflicting
    with pytest.raises(ValueError,match='MULTI_SOURCE_CHILD_ALIAS'):
        verify_multi_source_closure(artifacts,params,images)


def test_multi_closure_refuses_cross_source_pixel_alpha_role_collision():
    from pathlib import Path
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef, canonical_hash
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.operations.session.contracts import VersionBinding
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-source.json'
    first=WorkingImage.model_validate_json(path.read_bytes());second=first.model_copy(deep=True)
    second.pixels=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    second.alpha=first.pixels
    images=(first,second)
    frames=tuple(VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(
        artifact_id=uuid4(),sha256=canonical_hash(image)),canvas_hash=canonical_hash(image.canvas))
        for image in images)
    class Artifacts:
        def declared_contract_refs(self,contract,maximum):
            if contract=='WorkingImage':return tuple(frame.source for frame in frames)
            return tuple(ref for image in images for ref in (image.pixels,image.alpha))
    with pytest.raises(ValueError,match='MULTI_SOURCE_CHILD_ALIAS'):
        verify_multi_source_closure(Artifacts(),SimpleNamespace(frames=frames),images)


@pytest.mark.parametrize('operation',['optical.panorama','optical.focus_stack'])
def test_complete_adapter_with_controlled_registration(tmp_path,monkeypatch,operation):
    from contextlib import contextmanager
    from hashlib import sha256
    from uuid import uuid4
    import numpy as np
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.optical.multi_prepare import prepare_planar_stack,AutomaticMultiPrepared
    import photo_worker.operations.optical.multi_image_prepare as preparation
    @contextmanager
    def automatic(frames,alphas,reference,scratch,**kwargs):
        for name in ('registration_error_px','match_inlier_min'):kwargs.pop(name)
        with prepare_planar_stack(frames,alphas,[np.eye(3)]*2,reference,scratch,**kwargs) as prepared:
            yield AutomaticMultiPrepared(prepared,(),False)
    monkeypatch.setattr(preparation,'prepare_automatic_planar_stack',automatic)
    admission,ctx,params,images,grant_records,calls,_=installed(monkeypatch,operation)
    ctx.lease.execution_profile.update(backend='cpu',renderer_version='v2.2')
    ctx.lease.execution_profile['resource_estimate']['ram_bytes']=2**40
    progress=[];ctx.progress=lambda *value:progress.append(value)
    paths={}
    for ordinal,image in enumerate(images):
        path=tmp_path/f'input-{ordinal}.npy'
        np.save(path,np.full((80,100,3),2.,np.float32));paths[image.pixels.artifact_id]=path
        image.pixels.sha256=sha256(path.read_bytes()).hexdigest()
        params.frames[ordinal].source.sha256=sha256(canonical_bytes(image)).hexdigest()
    from dataclasses import replace
    grant_records[0]=replace(grant_records[0],params_jcs=canonical_bytes(params))
    base=artifacts_for(params,images)
    class Artifacts:
        def __init__(self,scratch):self.scratch=scratch;self.records=[]
        params=base.params
        contract=base.contract
        declared_contract_refs=base.declared_contract_refs
        @contextmanager
        def input(self,ref,**kwargs):yield SimpleNamespace(path=paths[ref.artifact_id])
        def save(self,data,role):
            ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(data).hexdigest())
            self.records.append((ref,data,role));return ref
        def stage(self,path,reservation,role,media_type):
            reservation.validate_file(path);return self.save(path.read_bytes(),role)
        def stage_contract(self,value,role):return self.save(canonical_bytes(value),role)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,32*1024**2,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch)
        result=execute(ctx,artifacts,admission,operation=operation)
        assert result.review_required and len(result.outputs)==1
        assert artifacts.records[-1][0]==result.outputs[0]
        assert result.bytes_written==sum(len(data) for _,data,_ in artifacts.records)
        assert len(calls)==6 and progress==[(0,3),(1,3),(2,3),(3,3)]
        assert not list(scratch.directory.iterdir())
        from photo_worker.v2.multi_adoption import verify_multi_result_metadata
        from photo_worker.v2.lens_bypass import _ref
        from photo_worker.v2.adoption import AdoptionInvalid
        records={ref.artifact_id:(data,role,'output') for ref,data,role in artifacts.records}
        params_raw=canonical_bytes(params)
        params_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(params_raw).hexdigest())
        records[params_ref.artifact_id]=(params_raw,'params','input')
        for binding,image in zip(params.frames,images):
            records[binding.source.artifact_id]=(canonical_bytes(image),'working_image','input')
        records[params.calibration.artifact_id]=(grant_records[0].calibration_raw,'calibration','input')
        for image in images:
            records[image.pixels.artifact_id]=(paths[image.pixels.artifact_id].read_bytes(),'pixels','input')
        class Reader:
            def evidence(self,ref):
                data,role,origin=records[ref.artifact_id]
                return SimpleNamespace(ref=ref,source=origin,role=role,bytes=len(data),
                    media_type='application/x-npy' if role in ('pixels','alpha','mask_pixels','warp_field') else 'application/json')
            def read_json(self,ref,maximum):return records[ref.artifact_id][0]
            def staged_outputs(self):
                return {_ref(ref) for ref,data,role in artifacts.records}
            def iter_bytes(self,ref,maximum):
                raw=records[ref.artifact_id][0]
                for offset in range(0,len(raw),65536):yield raw[offset:offset+65536]
        reader=Reader()
        metadata=verify_multi_result_metadata(reader,_ref(params_ref),_ref(result.outputs[0]),
            operation=operation,max_pixels=10000,check_active=lambda:None)
        assert metadata.params==params and metadata.report.status=='review'
        assert [r.source for r in metadata.registrations]==params.frames
        from photo_worker.v2.multi_replay import verify_admitted_multi_replay
        replayed=verify_admitted_multi_replay(reader,_ref(params_ref),_ref(result.outputs[0]),
            ctx=ctx,admission=admission,scratch_root=tmp_path,max_bytes=32*1024**2,ram_max_bytes=2**40)
        assert replayed.params==params and replayed.motion_fraction==0.
        from photo_worker.v2.adoption import prepare_v2_adoption
        inventory=prepare_v2_adoption(task_id=uuid4(),operation=operation,version='1',
            root=_ref(result.outputs[0]),reader=reader,params_ref=_ref(params_ref),check_active=lambda:None)
        expected_refs={str(ref.artifact_id) for ref,_,_ in artifacts.records}
        expected_refs.update(str(binding.source.artifact_id) for binding in params.frames)
        assert {row['artifact_id'] for row in inventory.inventory}==expected_refs
        assert len(inventory.inventory)==len(expected_refs)
        # Provenance role substitution must fail even when document bytes match.
        rgb_id=metadata.image.pixels.artifact_id
        raw,role,origin=records[rgb_id];records[rgb_id]=(raw,'mask_pixels',origin)
        with pytest.raises(AdoptionInvalid,match='OUTPUT_EVIDENCE_INVALID'):
            verify_multi_result_metadata(reader,_ref(params_ref),_ref(result.outputs[0]),
                operation=operation,max_pixels=10000,check_active=lambda:None)
