"""Source-bound culling measurements and bounded immutable feature packs."""
from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.collection import CollectionFeatureParams,FeaturePackParams,SceneFeatures,SceneFeaturePack,PackedSceneFeature
from photo_worker.contracts.images import WorkingImage,Stats20
from photo_worker.contracts.scene import Embedding384
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation,OperationFailure,OperationResult


def describe(ctx,artifacts):
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(CollectionFeatureParams)
        image=artifacts.contract(p.image,WorkingImage)
        embedding=artifacts.contract(p.embedding,Embedding384)
        stats=artifacts.contract(p.stats,Stats20)
        if embedding.image!=p.image or stats.source_image!=p.image or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color.encoding!='gamma24' or image.canvas.width*image.canvas.height>200000000:
            raise ValueError('CULL_FEATURE_SOURCE_MISMATCH')
        h,w=image.canvas.height,image.canvas.width
        estimate=ctx.lease.execution_profile.get('resource_estimate',{});ram=estimate.get('ram_bytes')
        if type(ram) is not int or ram<64*1024**2:raise OperationFailure('RESOURCE_OOM')
        with ExitStack() as stack:
            local=stack.enter_context(artifacts.input(image.pixels,suffix='.npy'))
            rgb=stack.enter_context(read_array(local.path,(h,w,3),'float32',ctx.check_active,unit_interval=True))
            scale=min(1.,1024/max(h,w))
            preview=cv2.resize(rgb,(max(1,round(w*scale)),max(1,round(h*scale))),interpolation=cv2.INTER_AREA)
            gray=cv2.cvtColor(preview,cv2.COLOR_RGB2GRAY)
            sharpness=float(cv2.Laplacian(gray,cv2.CV_32F).var())
            low=cv2.resize(gray,(32,32),interpolation=cv2.INTER_AREA)
            dct=cv2.dct(low)[:8,:8].reshape(-1);median=float(np.median(dct[1:]))
            phash=sum(int(value>median)<<(63-i) for i,value in enumerate(dct))
            clipped=0
            for row in range(0,h,256):
                ctx.check_active();block=rgb[row:row+256]
                clipped+=int(np.count_nonzero(np.any((block<=0)|(block>=1),axis=2)))
        result=SceneFeatures(item_id=p.item_id,image=p.image,content_sha256=image.canvas.source_sha256,
            phash64=f'{phash:016x}',dino=embedding.values,stats20=stats.values,captured_at=p.captured_at,
            clock_offset_seconds=p.clock_offset_seconds,sharpness=sharpness,blur=1/(1+1000*sharpness),
            clipping=clipped/(h*w),eyes='unknown')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.check_active();ref=artifacts.stage_contract(result,'scene_features')
    return OperationResult(outputs=[ref],review_required=True,bytes_written=len(canonical_bytes(result)))


def pack(ctx,artifacts):
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(FeaturePackParams);items=[]
        for ref in p.features:
            ctx.check_active();items.append(PackedSceneFeature(feature=ref,value=artifacts.contract(ref,SceneFeatures)))
        result=SceneFeaturePack(features=items)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.check_active();ref=artifacts.stage_contract(result,'scene_feature_pack')
    return OperationResult(outputs=[ref],bytes_written=len(canonical_bytes(result)))


def feature_operations(config):
    def described(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'scene_features'})) as artifacts:return describe(ctx,artifacts)
    def packed(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'scene_feature_pack'})) as artifacts:return pack(ctx,artifacts)
    return [Operation('collection.features','cpu',described),Operation('collection.features_pack','cpu',packed)]
