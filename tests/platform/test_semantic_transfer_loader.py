from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.semantic.load_inputs import PinnedSemanticArrayLoader
from photo_worker.operations.optical.color import to_working_linear
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.runtime.executor import OperationFailure
from test_image_denoise import Artifacts
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['valid','budget','protection','mutating_verifier','reference_canvas','target_canvas','sql_scope_denied','sql_scope_revoked','sql_scope_revoked_after_use'])
def test_pinned_semantic_loader_linear_conversion_and_cleanup(tmp_path,variant):
    a=Artifacts(tmp_path);params,intent,_=transfer_documents()
    reference=params.reference.model_copy(update={'source':a.image_ref,'canvas_hash':canonical_hash(a.image.canvas)})
    params=params.model_copy(update={'reference':reference,'target':reference})
    if variant=='reference_canvas':params=params.model_copy(update={'reference':reference.model_copy(update={'canvas_hash':'0'*64})})
    if variant=='target_canvas':params=params.model_copy(update={'target':reference.model_copy(update={'canvas_hash':'0'*64})})
    mask_ref=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    intent=intent.model_copy(update={'reference':reference,'reference_mask':mask_ref})
    path=tmp_path/'semantic-mask.npy';np.save(path,np.ones((20,24),np.float32))
    zero_path=tmp_path/'protection.npy';np.save(zero_path,np.zeros((20,24),np.float32))
    pixels=ArtifactRef(artifact_id=uuid4(),sha256='c'*64);zero=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    protection_ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    mask=MaskRef(artifact=pixels,canvas=a.image.canvas,transform_chain=a.image.transform_chain,
        selector='manual',confidence=1.,model_lock_id=None,protected=False)
    protection=mask.model_copy(update={'artifact':zero,'protected':variant!='protection'})
    docs={params.intent.artifact_id:intent,mask_ref.artifact_id:mask,protection_ref.artifact_id:protection}
    paths={a.pixel_ref.artifact_id:a.paths[a.pixel_ref.artifact_id],pixels.artifact_id:path,zero.artifact_id:zero_path}
    reads=[]
    @contextmanager
    def input(ref,**kwargs):
        reads.append(ref);yield SimpleNamespace(path=paths[ref.artifact_id])
    def verify_inputs(*args):
        if variant=="mutating_verifier":
            loader.reference_protection.artifact_id=uuid4()
            loader.target_protection.sha256="f"*64
    loader=PinnedSemanticArrayLoader(protection_ref,protection_ref,verify_inputs=verify_inputs)
    if variant in ('sql_scope_denied','sql_scope_revoked','sql_scope_revoked_after_use'):
        from photo_worker.operations.semantic.load_inputs import sql_pinned_semantic_array_loader
        from photo_worker.contracts.base import canonical_bytes
        from hashlib import sha256
        raw=canonical_bytes(params)
        lease=SimpleNamespace(command=SimpleNamespace(params=SimpleNamespace(sha256=sha256(raw).hexdigest())))
        scope_calls=[]
        def source_ready(*args,**kwargs):
            scope_calls.append(1)
            return (variant=='sql_scope_revoked' and len(scope_calls)<=2
                or variant=='sql_scope_revoked_after_use' and len(scope_calls)<=4)
        db=SimpleNamespace(semantic_source_ready=source_ready)
        loader=sql_pinned_semantic_array_loader(db,lease,raw,protection_ref,protection_ref,
            verify_color=lambda image:None)

    from dataclasses import FrozenInstanceError
    with pytest.raises(FrozenInstanceError):
        loader.verify_inputs=lambda *args:None
    protection_ref.artifact_id=uuid4()  # Constructor owns the original identity.
    profile={'resource_estimate':dict(ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=825169920)}
    if variant=='budget':profile['resource_estimate']['scratch_bytes']=0
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(scratch=scratch,input=input,contract=lambda ref,model:docs[ref.artifact_id])
        def consume():
            with loader(artifacts,params,a.image,a.image,profile,lambda:None) as result:
                np.testing.assert_array_equal(result.reference,to_working_linear(a.source,a.image.color))
                assert not result.reference.flags.writeable and not result.mask.flags.writeable
                assert np.all(result.mask==1) and np.all(result.target_protected==0)
        if variant in ('sql_scope_denied','sql_scope_revoked','sql_scope_revoked_after_use'):
            with pytest.raises(ValueError,match='SOURCE_SCOPE_STALE'):consume()
            assert len(reads)==(0 if variant=='sql_scope_denied' else 5)
        elif variant in ('reference_canvas','target_canvas'):
            with pytest.raises(ValueError,match='SEMANTIC_SOURCE_CHANGED'):consume()
            assert reads==[]
        elif variant=='budget':
            with pytest.raises(OperationFailure,match='SCRATCH_ADMISSION_EXCEEDED'):consume()
            assert reads==[]
        elif variant=='protection':
            with pytest.raises(ValueError,match='PROTECTION_BINDING_INVALID'):consume()
            assert reads==[]
        else:consume()
        assert not list(scratch.directory.iterdir())
