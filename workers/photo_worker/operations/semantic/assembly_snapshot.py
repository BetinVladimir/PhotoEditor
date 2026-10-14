"""Frozen input snapshot reader under the running CPU assembly lease."""
from copy import deepcopy
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import EditIntent
from photo_worker.operations.session.contracts import InputSnapshot
from photo_worker.v2.adoption import AdoptionInvalid


def sql_semantic_assembly_snapshot(database,ctx):
    if not callable(getattr(database,'semantic_assembly_snapshot',None)):
        raise ValueError('SEMANTIC_SNAPSHOT_AUTHORITY_REQUIRED')
    lease=deepcopy(ctx.lease)
    def scope():
        ctx.check_active()
        if ctx.lease!=lease:raise AdoptionInvalid('SEMANTIC_LEASE_CHANGED')
        rows=database.semantic_assembly_snapshot(deepcopy(lease))
        if not isinstance(rows,list) or not 1<=len(rows)<=5000:
            raise AdoptionInvalid('SEMANTIC_SNAPSHOT_SCOPE_STALE')
        return deepcopy(rows)
    def load(artifacts,intent):
        intent=EditIntent.model_validate_json(canonical_bytes(intent))
        rows=scope();pin=rows[0]
        keys=('snapshot_artifact_id','snapshot_sha','snapshot_id','snapshot_digest')
        if any(any(row.get(k)!=pin.get(k) for k in keys) or type(row.get('ordinal')) is not int or row['ordinal']!=n for n,row in enumerate(rows)):
            raise AdoptionInvalid('SEMANTIC_SNAPSHOT_EVIDENCE_INVALID')
        root=ArtifactRef(artifact_id=UUID(str(pin['snapshot_artifact_id'])),sha256=pin['snapshot_sha'])
        snapshot=artifacts.contract(root,InputSnapshot)
        snapshot=InputSnapshot.model_validate_json(canonical_bytes(snapshot))
        if (str(snapshot.snapshot_id)!=str(pin['snapshot_id']) or snapshot.snapshot_id!=intent.target_snapshot_id
                or snapshot.snapshot_digest!=pin['snapshot_digest'] or len(snapshot.items)!=len(rows)
                or canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))!=pin['snapshot_digest']):
            raise AdoptionInvalid('SEMANTIC_SNAPSHOT_CHANGED')
        for item,row in zip(snapshot.items,rows):
            ctx.check_active()
            if (item.ordinal!=row['ordinal'] or str(item.binding.asset_version_id)!=str(row['asset_version_id'])
                    or str(item.binding.source.artifact_id)!=str(row['source_id']) or item.binding.source.sha256!=row['source_sha']
                    or item.working_image!=item.binding.source):raise AdoptionInvalid('SEMANTIC_SNAPSHOT_CHANGED')
            image=artifacts.contract(item.binding.source,WorkingImage)
            if canonical_hash(image.canvas)!=item.binding.canvas_hash:raise AdoptionInvalid('SEMANTIC_SNAPSHOT_CANVAS_CHANGED')
        if scope()!=rows:raise AdoptionInvalid('SEMANTIC_SNAPSHOT_SCOPE_STALE')
        return snapshot
    return load
