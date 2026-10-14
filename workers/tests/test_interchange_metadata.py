from types import SimpleNamespace
from uuid import uuid4,uuid5
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_sources import ImportSources,ImportSourceEntry
from photo_worker.v2.interchange_metadata import MetadataReceipt,MetadataReceipts,qualified_metadata_mapper


def fixture(rating=3):
    workflow,project,identifier,version=uuid4(),uuid4(),uuid4(),uuid4()
    master=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    parsed=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    run=uuid5(workflow,'interchange:canonical-ingest:1')
    sources=ImportSources(schema_version=1,workflow_id=workflow,project_id=project,parsed=parsed,ingest_run_id=run,
        entries=[ImportSourceEntry(ordinal=0,entry_id=identifier,state='adopted',reason=None,asset_version_id=version,master=master)])
    receipts=MetadataReceipts(schema_version=1,workflow_id=workflow,project_id=project,parsed=parsed,
        collection_id=uuid5(workflow,'interchange:draft-collection:1'),entries=[MetadataReceipt(entry_id=identifier,
        asset_version_id=version,master=master,item_id=uuid5(run,'item:0'),rating=max(0,rating),flag='pick')])
    entry=SimpleNamespace(entry_id=identifier,rating=rating,pick='picked',crop=None)
    binding=VersionBinding(asset_version_id=version,source=master,canvas_hash='c'*64)
    return sources,receipts,entry,binding


@pytest.mark.parametrize('rating',[0,3,5,-1])
def test_metadata_exact_requires_matching_persisted_values(rating):
    sources,receipts,entry,binding=fixture(rating)
    fields=qualified_metadata_mapper(receipts,sources)(entry,binding)
    assert [(f.name,f.status) for f in fields]==[('rating','unsupported' if rating==-1 else 'exact'),('pick','exact'),('crop','exact')]
    entry.pick='rejected'
    with pytest.raises(AdoptionInvalid,match='CHANGED'):
        qualified_metadata_mapper(receipts,sources)(entry,binding)


def test_foreign_collection_or_item_receipt_is_rejected():
    for field in ['collection_id','item_id']:
        sources,receipts,entry,binding=fixture()
        if field=='collection_id':receipts.collection_id=uuid4()
        else:receipts.entries[0].item_id=uuid4()
        with pytest.raises(AdoptionInvalid):
            qualified_metadata_mapper(receipts,sources)


def test_color_label_requires_exact_collection_receipt():
    sources,receipts,entry,binding=fixture()
    entry.label='Blue'
    receipts.entries[0].label='Blue'
    fields=qualified_metadata_mapper(receipts,sources)(entry,binding)
    assert ('label','exact') in [(field.name,field.status) for field in fields]
    receipts.entries[0].label='Red'
    with pytest.raises(AdoptionInvalid,match='METADATA_CHANGED'):
        qualified_metadata_mapper(receipts,sources)(entry,binding)


def test_metadata_loader_queries_scoped_receipts_and_returns_owned_values(monkeypatch):
    from photo_worker.v2.interchange_metadata import load_import_metadata
    from photo_worker.v2 import interchange_import_sources
    from photo_worker.contracts.base import canonical_bytes
    sources,receipts,_,_=fixture()
    monkeypatch.setattr(interchange_import_sources,'load_import_sources',lambda *a,**k:sources)
    wire=receipts.model_dump(mode='json');calls=[];tenant=uuid4()
    def execute(statement,args):
        calls.append((statement,args));return SimpleNamespace(fetchone=lambda:(wire,))
    result=load_import_metadata(SimpleNamespace(execute=execute),tenant,sources.workflow_id,sources.parsed,b'{}')
    assert calls==[('SELECT photo.v30_interchange_metadata_receipts(%s,%s,%s)',(tenant,sources.workflow_id,'{}'))]
    wire['entries'][0]['rating']=0
    assert result[1].entries[0].rating==3
    assert canonical_bytes(result[0])==canonical_bytes(sources)


@pytest.mark.parametrize('ready',[False,True])
def test_metadata_loader_never_invents_pending_receipts(monkeypatch,ready):
    from photo_worker.v2.interchange_metadata import load_import_metadata
    from photo_worker.v2 import interchange_import_sources
    sources,_,_,_=fixture()
    monkeypatch.setattr(interchange_import_sources,'load_import_sources',lambda *a,**k:sources if ready else None)
    calls=[]
    def execute(*args):
        calls.append(args);return SimpleNamespace(fetchone=lambda:(None,))
    assert load_import_metadata(SimpleNamespace(execute=execute),uuid4(),sources.workflow_id,sources.parsed,b'{}') is None
    assert len(calls)==int(ready)


def test_metadata_loader_rejects_substituted_collection(monkeypatch):
    from photo_worker.v2.interchange_metadata import load_import_metadata
    from photo_worker.v2 import interchange_import_sources
    sources,receipts,_,_=fixture();receipts.collection_id=uuid4()
    monkeypatch.setattr(interchange_import_sources,'load_import_sources',lambda *a,**k:sources)
    db=SimpleNamespace(execute=lambda *a:SimpleNamespace(fetchone=lambda:(receipts.model_dump(mode='json'),)))
    with pytest.raises(AdoptionInvalid,match='METADATA_SCOPE'):
        load_import_metadata(db,uuid4(),sources.workflow_id,sources.parsed,b'{}')


def test_metadata_loader_propagates_revocation_or_cancellation_after_sql(monkeypatch):
    from photo_worker.v2.interchange_metadata import load_import_metadata
    from photo_worker.v2 import interchange_import_sources
    sources,receipts,_,_=fixture();cancelled=False
    monkeypatch.setattr(interchange_import_sources,'load_import_sources',lambda *a,**k:sources)
    def execute(*args):
        nonlocal cancelled
        cancelled=True
        return SimpleNamespace(fetchone=lambda:(receipts.model_dump(mode='json'),))
    def active():
        if cancelled:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        load_import_metadata(SimpleNamespace(execute=execute),uuid4(),sources.workflow_id,sources.parsed,b'{}',check_active=active)
