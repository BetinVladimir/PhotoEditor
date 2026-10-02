import math
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.collection import CullItem,CullParams,CullRecommendations,SceneFeatures,SceneGroups
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _normalize(values):
    a=np.asarray(values,np.float64);lo,hi=np.percentile(a,[5,95]);return np.full(len(a),.5) if hi<=lo+1e-12 else np.clip((a-lo)/(hi-lo),0,1)


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(CullParams);groups=artifacts.contract(p.groups,SceneGroups);features=[artifacts.contract(r,SceneFeatures) for r in p.features];by={f.item_id:f for f in features}
        if set(by)!={i.item_id for i in groups.items} or {(i.feature.artifact_id,i.feature.sha256) for i in groups.items}!={(r.artifact_id,r.sha256) for r in p.features}:raise ValueError('Cull feature binding mismatch')
        if not set(p.manual_picks+p.intentional_closed)<=set(by):raise ValueError('Unknown cull override')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    scene_items={}
    for item in groups.items:scene_items.setdefault(item.scene_id,[]).append(item)
    scored={};eye_review={};ranks={}
    for scene_id,items in scene_items.items():
        fs=[by[i.item_id] for i in items];sharp=_normalize([f.sharpness for f in fs]);blur=_normalize([f.blur for f in fs]);clip=_normalize([f.clipping for f in fs]);known=[f.eyes!='unknown' for f in fs]
        eye=np.array([1 if f.eyes=='open' or f.item_id in p.intentional_closed else 0 for f in fs],float)
        for n,(item,f) in enumerate(zip(items,fs)):
            weights=[.45,.25,.15];values=[sharp[n],1-blur[n],1-clip[n]]
            if known[n]:weights.append(.15);values.append(eye[n])
            score=float(np.dot(weights,values)/sum(weights));scored[item.item_id]=score;eye_review[item.item_id]=f.eyes=='unknown' or (f.eyes=='closed' and f.item_id not in p.intentional_closed)
        for rank,item in enumerate(sorted(items,key=lambda i:(-scored[i.item_id],str(i.item_id))),1):ranks[item.item_id]=rank
    total=len(features);target=p.target_count if p.target_count is not None else max(1,round(total*p.target_fraction));manual=set(p.manual_picks);warnings=[]
    if target<len(manual):target=len(manual);warnings.append('target_below_manual_picks')
    remaining=max(0,target-len(manual));counts={s:sum(i.item_id not in manual for i in v) for s,v in scene_items.items()};den=sum(counts.values());quotas={s:0 for s in counts}
    if den and remaining:
        raw={s:remaining*c/den for s,c in counts.items()};quotas={s:min(counts[s],math.floor(v)) for s,v in raw.items()};left=remaining-sum(quotas.values())
        for s in sorted(counts,key=lambda x:(-(raw[x]-math.floor(raw[x])),x)):
            if left and quotas[s]<counts[s]:quotas[s]+=1;left-=1
    selected=set(manual)
    for scene,items in scene_items.items():selected.update(i.item_id for i in sorted((x for x in items if x.item_id not in manual),key=lambda x:(-scored[x.item_id],str(x.item_id)))[:quotas[scene]])
    if len(selected)<target:warnings.append('selection_shortage')
    if any(by[i].eyes=='unknown' for i in by):warnings.append('unknown_eye_state')
    if any(by[i].eyes=='closed' and i not in p.intentional_closed for i in by):warnings.append('closed_eye_review')
    items=[CullItem(item_id=i.item_id,scene_id=i.scene_id,score=scored[i.item_id],rank=ranks[i.item_id],selected=i.item_id in selected,manual=i.item_id in manual,eye_review=eye_review[i.item_id]) for i in sorted(groups.items,key=lambda x:str(x.item_id))]
    result=CullRecommendations(groups=p.groups,target_count=target,selected_count=len(selected),items=items,warnings=warnings);ref=artifacts.stage_contract(result,'cull_recommendations');return OperationResult(outputs=[ref],review_required=True,bytes_written=len(canonical_bytes(result)))


def cull_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'cull_recommendations'})) as a:return execute(ctx,a)
    return [Operation('collection.cull','cpu',run)]
