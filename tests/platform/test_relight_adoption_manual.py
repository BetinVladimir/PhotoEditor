from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2 import relight_adoption_manual as module
from photo_worker.contracts.base import canonical_hash
from test_relight_inputs import inputs


@pytest.mark.parametrize('protected', [False, True])
def test_completed_manual_missing_origin_refuses(monkeypatch, protected):
    params, _, _, mask, *_ = inputs()
    mask = mask.model_copy(update={'selector': 'manual', 'protected': protected})
    ref = (params.protected if protected else params.subject).model_copy(update={'sha256': canonical_hash(mask)})
    params = params.model_copy(update={'protected' if protected else 'subject': ref})
    monkeypatch.setattr(module, 'load_relight_adoption_source', lambda *a, **k: {})
    monkeypatch.setattr(module, 'relight_adoption_source_checkpoint', lambda *a, **k: lambda: None)
    calls = []
    def execute(sql, args):
        calls.append(args)
        return SimpleNamespace(fetchone=lambda: (None,))
    with pytest.raises(ValueError, match='RELIGHT_ADOPTION_MANUAL_STALE'):
        module.resolve_relight_adoption_manual(SimpleNamespace(execute=execute), None,
            uuid4(), uuid4(), SimpleNamespace(attempt=1, fence=2, result_id=uuid4()),
            params, mask, protected=protected, check_active=lambda: None)
    assert len(calls) == 1 and calls[0][-1] is protected and calls[0][-2] is None


def test_completed_manual_non_boolean_branch_refuses():
    params, _, _, mask, *_ = inputs()
    with pytest.raises(ValueError, match='RELIGHT_ADOPTION_MANUAL_INVALID'):
        module.resolve_relight_adoption_manual(None, None, uuid4(), uuid4(),
            SimpleNamespace(), params, mask, protected=1, check_active=lambda: None)


@pytest.mark.parametrize('mode', ['ok', 'target', 'guard', 'budget', 'changed'])
def test_completed_manual_raster_replay_with_budgeted_arrays(monkeypatch, tmp_path, mode):
    import io
    import numpy as np
    from photo_worker.contracts.base import ArtifactRef, canonical_bytes
    from photo_worker.contracts.manual_mask import ManualMask, MaskRasterParams, RasterizedMask
    from photo_worker.v2.adoption import ArtifactEvidence
    params, source, _, target, guard, *_ = inputs()
    source = source.model_copy(update={'canvas': source.canvas.model_copy(update={'width':5,'height':5})})
    source_ref = params.base.source.model_copy(update={'sha256':canonical_hash(source)})
    params = params.model_copy(update={'base':params.base.model_copy(update={'source':source_ref})})
    blobs = {}
    def put(raw):
        from hashlib import sha256
        ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
        blobs[ref.artifact_id] = raw
        return ref
    def pixels(forge):
        array = np.zeros((5,5),np.float32)
        if forge:array[0,0] = .5
        stream = io.BytesIO();np.save(stream,array,allow_pickle=False)
        return put(stream.getvalue())
    target = target.model_copy(update={'canvas':source.canvas,'selector':'manual','artifact':pixels(mode=='target')})
    guard = guard.model_copy(update={'canvas':source.canvas,'selector':'manual','artifact':pixels(mode=='guard')})
    vectors = ManualMask(canvas=source.canvas,source_sha256=source.canvas.source_sha256,shapes=[])
    original = MaskRasterParams(image=source_ref,inline_vectors=vectors)
    original_ref = put(canonical_bytes(original))
    root = RasterizedMask(image=source_ref,vectors=original_ref,target=target,protected=guard,
        target_png=put(b"target-png"),protected_png=put(b"guard-png"))
    root_ref = put(canonical_bytes(root))
    record = {key:module.ArtifactRef.parse(ref.model_dump(mode='json')) for key,ref in [('root',root_ref),('params',original_ref)]}
    calls=[]
    def resolve(*a,**k):
        calls.append(1)
        if mode=='changed' and len(calls)>1:raise ValueError('ORIGIN_REVOKED')
        return record,root,original
    monkeypatch.setattr(module,'resolve_relight_adoption_manual',resolve)
    class Reader:
        def evidence(self,ref):return ArtifactEvidence(ref,'application/x-npy',len(blobs[ref.artifact_id]),None,'input')
        def iter_bytes(self,ref,maximum):yield blobs[ref.artifact_id]
    monkeypatch.setattr(module,'_document',lambda reader,ref,model,origin:source)
    call=lambda:module.verify_relight_adoption_manual(None,Reader(),uuid4(),uuid4(),SimpleNamespace(),
        params,guard,protected=True,scratch_root=tmp_path,max_bytes=1 if mode=='budget' else 10000,check_active=lambda:None)
    if mode=='ok':assert call() is True and len(calls)==2
    else:
        with pytest.raises(ValueError):call()
    assert list(tmp_path.iterdir())==[]
