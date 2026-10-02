from datetime import timezone
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.collection import GroupParams,GroupedItem,SceneFeatures,SceneGroups
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _hamming(a,b):return (int(a,16)^int(b,16)).bit_count()
def _time(v):return None if v.captured_at is None else v.captured_at.astimezone(timezone.utc).timestamp()+v.clock_offset_seconds
def _cos(a,b):return float(np.dot(a,b))
def _scene_distance(a,b,scale):
    dino=1-_cos(a.dino,b.dino);stats=float(np.sqrt(np.mean(((np.asarray(a.stats20)-b.stats20)/scale)**2)));return .7*dino+.3*min(stats,1.)


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(GroupParams);features=[artifacts.contract(r,SceneFeatures) for r in p.features]
        if len({f.item_id for f in features})!=len(features) or len({(r.artifact_id,r.sha256) for r in p.features})!=len(p.features):raise ValueError('Duplicate group feature')
        by_id={f.item_id:(f,r) for f,r in zip(features,p.features)};known=set(by_id);separate={frozenset((x.left,x.right)) for x in p.forced_separate}
        if any(x.left not in known or x.right not in known for x in p.forced_separate):raise ValueError('Unknown separate item')
        manual={}
        for group in p.manual_groups:
            if len(group.item_ids)!=len(set(group.item_ids)) or not set(group.item_ids)<=known:raise ValueError('Invalid manual group')
            if any(i in manual for i in group.item_ids) or any(frozenset(pair) in separate for pair in __import__('itertools').combinations(group.item_ids,2)):raise ValueError('Conflicting manual group')
            for i in group.item_ids:manual[i]=group.group_id
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ordered=sorted(features,key=lambda f:str(f.item_id));ref_by={f.item_id:r for f,r in zip(features,p.features)}
    def clustered(predicate,prefix,seed_groups=None):
        clusters=[] if seed_groups is None else [list(g) for g in seed_groups]
        assigned={x.item_id for g in clusters for x in g}
        for f in ordered:
            if f.item_id in assigned:continue
            candidates=[c for c in clusters if all(frozenset((f.item_id,x.item_id)) not in separate and predicate(f,x) for x in c)]
            if candidates:min(candidates,key=lambda c:str(min(x.item_id for x in c))).append(f)
            else:clusters.append([f])
        return clusters,{x.item_id:f'{prefix}:{min(str(y.item_id) for y in c)}' for c in clusters for x in c}
    manual_clusters=[]
    for gid in sorted(set(manual.values())):manual_clusters.append([by_id[i][0] for i in sorted((x for x,v in manual.items() if v==gid),key=str)])
    _,exact=clustered(lambda a,b:a.content_sha256==b.content_sha256,'exact',manual_clusters)
    def burst(a,b):
        ta,tb=_time(a),_time(b);return ta is not None and tb is not None and abs(ta-tb)<=p.burst_seconds and _hamming(a.phash64,b.phash64)<=p.phash_distance and _cos(a.dino,b.dino)>=p.dino_cosine
    _,bursts=clustered(burst,'burst',manual_clusters)
    stats_scale=np.maximum(np.std(np.asarray([f.stats20 for f in features],np.float64),axis=0),.05)
    scenes,scene=clustered(lambda a,b:_scene_distance(a,b,stats_scale)<=p.scene_distance,'scene',manual_clusters)
    warnings=[]
    if any(_time(f) is None for f in features):warnings.append('missing_capture_time')
    if manual_clusters or separate:warnings.append('manual_override')
    items=[GroupedItem(item_id=f.item_id,feature=ref_by[f.item_id],exact_group_id=exact[f.item_id],burst_group_id=bursts[f.item_id],scene_id=scene[f.item_id]) for f in ordered]
    result=SceneGroups(items=items,exact_group_count=len(set(exact.values())),burst_group_count=len(set(bursts.values())),scene_count=len(scenes),warnings=warnings);ref=artifacts.stage_contract(result,'scene_groups');return OperationResult(outputs=[ref],review_required=bool(warnings),bytes_written=len(canonical_bytes(result)))


def group_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'scene_groups'})) as a:return execute(ctx,a)
    return [Operation('collection.group','cpu',run)]
