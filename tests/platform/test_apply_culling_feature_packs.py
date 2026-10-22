"""Authored culling pack regressions; do not treat as executed evidence."""
from datetime import datetime,timezone
from uuid import uuid4
import hashlib
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.collection import SceneFeatures,SceneFeaturePack,PackedSceneFeature,FeaturePackParams,GroupParams,CullParams
from photo_worker.operations.collection.feature_inputs import load_features
from photo_worker.operations.collection.features import pack
from test_collection_group_cull import feature,Artifacts
from test_asset_adapters import context_for


def test_pack_preserves_exact_artifact_identity_and_honest_eye_state():
    values=[feature(i,0,datetime.now(timezone.utc),'unknown') for i in range(3)]
    artifacts=Artifacts(values,None);artifacts.value=FeaturePackParams(features=artifacts.refs)
    ctx=context_for(artifacts.refs[0],'collection.features_pack');ctx.lease.execution_profile['backend']='cpu'
    batch=pack(ctx,artifacts).outputs[0]
    params=GroupParams(feature_batches=[batch]);refs,loaded=load_features(artifacts,params)
    assert refs==artifacts.refs and loaded==values and all(f.eyes=='unknown' for f in loaded)
    assert not params.features


def test_mixed_legacy_and_batch_inputs_and_duplicate_batches_rejected():
    v=feature(0,0,datetime.now(timezone.utc));a=Artifacts([v],None)
    batch=SceneFeaturePack(features=[PackedSceneFeature(feature=a.refs[0],value=v)])
    b=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(batch)).hexdigest());a.contracts[b.artifact_id]=batch
    with pytest.raises(ValidationError):GroupParams(features=a.refs,feature_batches=[b])
    with pytest.raises(ValueError,match='Duplicate'):load_features(a,GroupParams(feature_batches=[b,b]))
    with pytest.raises(ValidationError):SceneFeaturePack(features=[PackedSceneFeature(feature=a.refs[0],value=v)]*2)
    with pytest.raises(ValidationError):GroupParams()
    with pytest.raises(ValidationError):CullParams(groups=b,feature_batches=[b])


def test_eighty_feature_pack_is_bounded_below_item_contract_limit():
    refs=[];values=[]
    for i in range(80):
        v=feature(i%8,0,datetime.now(timezone.utc),'unknown');v.blur=.1;v.clipping=.1
        ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(v)).hexdigest());values.append(PackedSceneFeature(feature=ref,value=v));refs.append(ref)
    packed=SceneFeaturePack(features=values)
    assert len(canonical_bytes(packed))<1048576
    with pytest.raises(ValidationError):FeaturePackParams(features=refs+[refs[0]])


def _measurements(tmp_path):
    from test_image_color_adapter import Artifacts as ImageArtifacts,context
    from photo_worker.contracts.scene import Embedding384
    from photo_worker.contracts.images import Stats20
    from photo_worker.contracts.collection import CollectionFeatureParams
    a=ImageArtifacts(tmp_path,False)
    embedding=Embedding384(image=a.image_ref,values=[1.]+[0.]*383,model_id='dinov2_vits14',model_lock_id='a'*64,backend='mps',preprocessing_id='dinov2_pad518_v1',source_clip_fraction=0.)
    stats=Stats20(source_image=a.image_ref,feature_version='stats20_v1',values=[0.]*20,coverage_bins=[False]*512,mask_confidence=1.,warnings=[])
    eref,sref=a.add(embedding),a.add(stats)
    params=CollectionFeatureParams(item_id=uuid4(),image=a.image_ref,embedding=eref,stats=sref)
    a.raw=params.model_dump(mode='json')
    def staged(value,role):
        a.staged.append((role,value));return a.add(value)
    a.stage_contract=staged
    return a,context(a),eref,sref


def test_measurements_read_actual_pixels_and_keep_eyes_unknown(tmp_path):
    from photo_worker.operations.collection.features import describe
    a,ctx,_,_=_measurements(tmp_path)
    result=describe(ctx,a);value=a.staged[0][1]
    assert len(result.outputs)==1 and result.review_required
    assert value.image==a.image_ref and value.content_sha256==a.image.canvas.source_sha256
    assert value.eyes=='unknown' and value.captured_at is None
    assert len(value.phash64)==16 and value.sharpness>=0 and value.blur==pytest.approx(1/(1+1000*value.sharpness))
    assert value.clipping==0.


def test_foreign_embedding_or_stats_rejected_before_pixel_io(tmp_path):
    from photo_worker.operations.collection.features import describe
    from photo_worker.runtime.executor import OperationFailure
    a,ctx,eref,sref=_measurements(tmp_path)
    foreign=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    a.contracts[eref.artifact_id]=a.contracts[eref.artifact_id].model_copy(update={'image':foreign})
    def forbidden(*args,**kwargs):raise AssertionError('pixel IO before source binding validation')
    a.input=forbidden
    with pytest.raises(OperationFailure) as error:describe(ctx,a)
    assert error.value.error.code=='INPUT_INVALID' and not a.staged
