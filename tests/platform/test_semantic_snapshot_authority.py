import json
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.semantic_snapshot_authority import load_semantic_snapshot
from test_semantic_plan_adoption import case
from test_semantic_transfer_contracts import transfer_documents
from test_v2_adoption import ROOT


@pytest.mark.parametrize('variant',['valid','empty','ordinal','pin','source','intent','bytes','canvas','cancel','revoked_after_read','changed_after_read'])
def test_snapshot_live_catalog_rows_and_owned_documents(variant):
    reader,_,_,snapshot,_=case();_,intent,_=transfer_documents()
    image=WorkingImage.model_validate_json(json.dumps(json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]))
    source=ContractRef(artifact_id=uuid4(),sha256=canonical_hash(image))
    reader.add(ArtifactRef(source.artifact_id,source.sha256),body=image,source='input')
    for item in snapshot.items:
        item.binding.source=source;item.working_image=source;item.binding.canvas_hash=canonical_hash(image.canvas)
    if variant=='canvas':snapshot.items[0].binding.canvas_hash='f'*64
    snapshot.snapshot_digest=canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    root=ArtifactRef(uuid4(),canonical_hash(snapshot));reader.add(root,body=snapshot,source='input')
    intent.target_snapshot_id=snapshot.snapshot_id
    if variant=='intent':intent.target_snapshot_id=uuid4()
    rows=[(root.artifact_id,root.sha256,snapshot.snapshot_id,snapshot.snapshot_digest,item.ordinal,
        item.binding.asset_version_id,source.artifact_id,source.sha256) for item in snapshot.items]
    if variant=='empty':rows=[]
    if variant=='ordinal':rows[0]=(*rows[0][:4],1,*rows[0][5:])
    if variant=='pin':rows[1]=(uuid4(),*rows[1][1:])
    if variant=='source':rows[0]=(*rows[0][:6],uuid4(),rows[0][7])
    if variant=='bytes':reader.body[root]+=b' '
    tenant,task=uuid4(),uuid4();context=SimpleNamespace(attempt=1,fence=2,result_id=uuid4())
    class Connection:
        autocommit=True
        calls=0
        def execute(self,query,args):
            assert 'v27_semantic_assembly_snapshot' in query and args==(tenant,task,1,2,context.result_id)
            self.calls+=1
            if self.calls==2 and variant=='revoked_after_read':
                return SimpleNamespace(fetchall=lambda:[])
            if self.calls==2 and variant=='changed_after_read':
                changed=[(*rows[0][:6],uuid4(),rows[0][7]),*rows[1:]]
                return SimpleNamespace(fetchall=lambda:changed)
            return SimpleNamespace(fetchall=lambda:rows)
    def active():
        if variant=='cancel':raise InterruptedError('cancelled')
    def run():return load_semantic_snapshot(Connection(),tenant,task,context,reader,intent,check_active=active)
    if variant=='valid':
        result=run();snapshot.items[0].binding.canvas_hash='e'*64
        assert result.items[0].binding.canvas_hash==canonical_hash(image.canvas)
    elif variant=='cancel':
        with pytest.raises(InterruptedError):run()
    else:
        with pytest.raises(AdoptionInvalid,match='SEMANTIC_SNAPSHOT_'):run()
