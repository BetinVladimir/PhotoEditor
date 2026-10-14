from types import SimpleNamespace
import pytest
from test_semantic_transfer_contracts import transfer_documents
from photo_worker.operations.semantic.assembly_snapshot import sql_semantic_assembly_snapshot
from photo_worker.v2.adoption import AdoptionInvalid


@pytest.mark.parametrize('changed',[False,True])
def test_denied_snapshot_does_not_read_artifacts(changed):
    _,intent,_=transfer_documents()
    calls=[]
    ctx=SimpleNamespace(lease=SimpleNamespace(fence=1),check_active=lambda:None)
    database=SimpleNamespace(semantic_assembly_snapshot=lambda lease:calls.append(lease.fence) or [])
    load=sql_semantic_assembly_snapshot(database,ctx)
    if changed:ctx.lease.fence=2
    def forbidden(*args):raise AssertionError('Denied scope must not perform artifact IO')
    with pytest.raises(AdoptionInvalid,match='LEASE_CHANGED' if changed else 'SCOPE_STALE'):
        load(SimpleNamespace(contract=forbidden),intent)
    assert calls==([] if changed else [1])


@pytest.mark.parametrize('variant',['valid','revoked','source','canvas'])
def test_snapshot_contents_and_post_read_authority(variant):
    import json
    from copy import deepcopy
    from uuid import uuid4
    from test_semantic_plan_adoption import case
    from test_v2_adoption import ROOT
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.base import ArtifactRef,canonical_hash
    _,_,_,snapshot,_=case();_,intent,_=transfer_documents()
    image=WorkingImage.model_validate_json(json.dumps(json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]))
    source=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(image))
    for item in snapshot.items:
        item.binding.source=source;item.working_image=source;item.binding.canvas_hash=canonical_hash(image.canvas)
    if variant=='canvas':snapshot.items[0].binding.canvas_hash='f'*64
    snapshot.snapshot_digest=canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    root=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(snapshot))
    intent.target_snapshot_id=snapshot.snapshot_id
    rows=[dict(snapshot_artifact_id=str(root.artifact_id),snapshot_sha=root.sha256,snapshot_id=str(snapshot.snapshot_id),snapshot_digest=snapshot.snapshot_digest,ordinal=item.ordinal,asset_version_id=str(item.binding.asset_version_id),source_id=str(source.artifact_id),source_sha=source.sha256) for item in snapshot.items]
    if variant=='source':rows[0]['source_id']=str(uuid4())
    reads=[];checks=[]
    def authority(lease):
        checks.append(1)
        return [] if variant=='revoked' and reads else deepcopy(rows)
    def contract(ref,model):
        reads.append(ref)
        return deepcopy(snapshot if ref==root else image)
    ctx=SimpleNamespace(lease=SimpleNamespace(fence=1),check_active=lambda:None)
    load=sql_semantic_assembly_snapshot(SimpleNamespace(semantic_assembly_snapshot=authority),ctx)
    if variant=='valid':
        result=load(SimpleNamespace(contract=contract),intent)
        assert result==snapshot and result is not snapshot and len(checks)==2
    else:
        with pytest.raises(AdoptionInvalid):load(SimpleNamespace(contract=contract),intent)
