from uuid import UUID,uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.style import Disabled
from photo_worker.operations.portrait.beauty import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_image_color_adapter import Artifacts,context
from test_beauty_prepare import inputs
from test_beauty import params


def setup(root,enabled=True):
    a=Artifacts(root,False);rgb,image,faces,_=inputs()
    a.rgb=rgb;a.image=image.model_copy(update={'pixels':a.array(root,'beauty-source',rgb)})
    a.image_ref=a.add(a.image)
    body=a.resolved.model_dump(exclude={'hash'})
    body['params']['image']=a.image_ref
    body['strength']=.5;body['modules']['beauty']=params() if enabled else Disabled()
    a.resolved=ResolvedStyle.create(**body)
    bindings=[]
    for i,entry in enumerate(faces):
        mask=MaskRef(artifact=a.array(root,'skin-'+str(i),entry.skin_probability),canvas=image.canvas,transform_chain=image.transform_chain,
            selector='skin',confidence=1.,model_lock_id=None,object_id=entry.face.object_id,protected=False)
        bindings.append(dict(landmarks=a.add(entry.face).model_dump(mode='json'),skin=mask.model_dump(mode='json')))
    a.raw=dict(image=a.image_ref.model_dump(mode='json'),resolved=a.add(a.resolved).model_dump(mode='json'),faces=bindings)
    ctx=context(a);ctx.lease.command.inputs[:]=[r for r in ctx.lease.command.inputs if r is not None]
    ctx.lease.execution_profile['resource_estimate']['ram_bytes']=1073741824
    return a,ctx


def test_beauty_adapter_applies_effective_params_and_reports_faces(tmp_path):
    a,ctx=setup(tmp_path);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,20*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    report=a.staged[1][1]
    assert report.requested.smoothing==.5 and report.effective.smoothing==.25
    assert report.effective.tone_evenness==.2 and report.effective.texture_keep==.9
    assert len(report.faces)==2 and all(f.applied for f in report.faces)
    assert not np.array_equal(a.output,a.rgb) and len(result.outputs)==3
    assert a.staged[0][1].canvas==a.image.canvas and not result.review_required
    assert ctx.snapshot_progress()==dict(done=3,total=3)


def test_disabled_beauty_skips_pixel_io_and_keeps_input(tmp_path):
    a,ctx=setup(tmp_path,False);root=tmp_path/'scratch';root.mkdir()
    def no_input(*args,**kwargs):raise AssertionError('Unexpected pixel download')
    a.input=no_input
    with ScratchSpace(root,uuid4(),uuid4(),1,20*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    assert len(result.outputs)==2 and not a.uploads
    assert a.staged[0][1]==a.image and a.staged[1][1].warnings==['module_disabled']


def test_wrong_face_mask_rejected_before_staging(tmp_path):
    a,ctx=setup(tmp_path);a.raw['faces'][0]['skin']['object_id']=str(uuid4())
    with pytest.raises(OperationFailure) as error:execute(ctx,a)
    assert error.value.error.code=='INPUT_INVALID' and not a.uploads


def test_beauty_accepts_faces_from_landmark_collection(tmp_path):
    a,ctx=setup(tmp_path);root=tmp_path/'scratch';root.mkdir()
    individual=[a.contracts[UUID(entry['landmarks']['artifact_id'])] for entry in a.raw['faces']]
    collection=FaceLandmarks(image=a.image_ref,faces=a.image_ref,landmarks=individual,
        model_id='fan2d',model_lock_id='0'*64,backend='cpu',preprocessing_id='fan2d_crop256_v1',
        mean_confidence=1.,warnings=[])
    collection_ref=a.add(collection)
    for entry,face in zip(a.raw['faces'],individual):
        entry['landmarks']=collection_ref.model_dump(mode='json')
        entry['object_id']=str(face.object_id)
    with ScratchSpace(root,uuid4(),uuid4(),1,20*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch;result=execute(ctx,a)
    assert len(result.outputs)==3
    assert len(a.staged[1][1].faces)==2 and all(face.applied for face in a.staged[1][1].faces)
