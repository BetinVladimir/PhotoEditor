from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.semantic.preview_adopt_stage import stage_shown_preview_image


def test_shown_preview_staging_rejects_wrong_document_hash_before_io():
    class Forbidden:
        def input(self,*args,**kwargs):raise AssertionError('unbound source IO')
    with pytest.raises(ValueError,match='SEMANTIC_PREVIEW_ADOPTION_INVALID'):
        stage_shown_preview_image(Forbidden(),ArtifactRef(artifact_id=uuid4(),sha256='a'*64),b'{}',
            verify_admission=lambda:True,check_active=lambda:None,max_output_bytes=2**40)


@pytest.mark.parametrize('failure',[None,'hdr','pixel_sha','image_sha','revoke_after_pixels','revoke_after_image',
    'nan','infinity','negative','wrong_dtype','wrong_shape','trailing','truncated','source_hash',
    'manifest_mutation','admission_1','admission_2','admission_3','admission_4','admission_5'])
def test_shown_preview_staging_copies_exact_npy_and_preserves_image(tmp_path,failure):
    from contextlib import contextmanager
    from hashlib import sha256
    import json
    from pathlib import Path
    from types import SimpleNamespace
    import numpy as np
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.images import WorkingImage
    fixture=Path(__file__).resolve().parents[1]/'backend/internal/review/testdata/working-images-python.json'
    image=WorkingImage.model_validate_json(canonical_bytes(json.loads(fixture.read_text())[0]))
    original=tmp_path/'source.npy'
    shape=(image.canvas.height,image.canvas.width,3)
    values=np.full(shape,2.5 if failure=='hdr' else .5,dtype=np.float32)
    if failure=='nan':values[0,0,0]=np.nan
    if failure=='infinity':values[0,0,0]=np.inf
    if failure=='negative':values[0,0,0]=-.01
    if failure=='wrong_dtype':values=values.astype(np.float64)
    if failure=='wrong_shape':values=values[:-1]
    np.save(original,values,allow_pickle=False)
    if failure=='trailing':
        with original.open('ab') as stream:stream.write(b'x')
    if failure=='truncated':original.write_bytes(original.read_bytes()[:-1])
    source_bytes=original.read_bytes()
    image=image.model_copy(update={'pixels':ArtifactRef(artifact_id=uuid4(),sha256='0'*64 if failure=='source_hash' else sha256(source_bytes).hexdigest())})
    raw=canonical_bytes(image)
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    staged=[];active=True;opened=0;closed=0;admissions=0
    class Artifacts:
        @contextmanager
        def input(self,pixels,**kwargs):
            nonlocal opened,closed
            assert pixels==image.pixels
            opened+=1
            try:yield SimpleNamespace(path=original)
            finally:closed+=1
        def stage(self,path,reservation,role,media):
            nonlocal active
            assert role=='pixels' and media=='application/x-npy'
            copied=path.read_bytes()
            assert copied==source_bytes
            staged.append(role)
            if failure=='revoke_after_pixels':active=False
            return ArtifactRef(artifact_id=uuid4(),sha256='0'*64 if failure=='pixel_sha' else sha256(copied).hexdigest())
        def stage_contract(self,value,role):
            nonlocal active
            assert active and role=='working_image'
            assert value.model_copy(update={'pixels':image.pixels})==image
            staged.append(role)
            if failure=='revoke_after_image':active=False
            digest=sha256(canonical_bytes(value)).hexdigest()
            if failure=='manifest_mutation':value.color.icc_sha256='0'*64
            return ArtifactRef(artifact_id=uuid4(),sha256='0'*64 if failure=='image_sha' else digest)
    def check_active():
        if not active:raise ValueError('revoked')
    def admitted():
        nonlocal admissions
        admissions+=1
        return failure!=f'admission_{admissions}'
    def run():
        from photo_worker.runtime.scratch import ScratchSpace
        with ScratchSpace(tmp_path,uuid4(),uuid4(),1,len(source_bytes),min_free_bytes=0) as scratch:
            artifacts=Artifacts();artifacts.scratch=scratch
            return stage_shown_preview_image(artifacts,ref,raw,verify_admission=admitted,
                check_active=check_active,max_output_bytes=2**40)
    if failure in (None,'hdr'):
        result=run()
        assert result.pixels.sha256==image.pixels.sha256
        assert result.bytes_written>len(source_bytes) and staged==['pixels','working_image']
    else:
        with pytest.raises(ValueError):run()
        if failure in ('pixel_sha','revoke_after_pixels','admission_4'):assert staged==['pixels']
        elif failure in ('image_sha','manifest_mutation','revoke_after_image','admission_5'):assert staged==['pixels','working_image']
        else:assert staged==[]
    assert opened==closed==(0 if failure=='admission_1' else 1)
