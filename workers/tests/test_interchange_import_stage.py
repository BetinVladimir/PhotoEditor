import json
from hashlib import sha256
from uuid import uuid4, uuid5
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.v2.interchange_import_sources import ImportSources
from photo_worker.v2.interchange_import_stage import prepare_import_stage, publish_import_proposal
from test_interchange_ingest_snapshot import fixture


def test_failed_and_unsupported_images_are_all_reported_without_recipe_io():
    body,_=fixture()
    parsed=json.loads(body)
    entries=[{'ordinal':index,'entry_id':entry['entry_id'],'state':'rejected' if index==0 else 'unsupported',
        'reason':'asset.decode:failed' if index==0 else 'unsupported_image_format','asset_version_id':None,'master':None}
        for index,entry in enumerate(parsed['manifest']['entries'])]
    workflow = uuid4()
    sources=ImportSources.model_validate_json(canonical_bytes({'schema_version':1,'workflow_id':str(workflow),'project_id':str(uuid4()),
        'parsed':{'artifact_id':str(uuid4()),'sha256':sha256(body).hexdigest()},
        'ingest_run_id':str(uuid5(workflow,'interchange:canonical-ingest:1')),'entries':entries}))
    def no_io(*args):
        raise AssertionError('terminal source attempted recipe/image IO')
    def publish(raw,*args):
        return ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    result=prepare_import_stage(tenant_id=uuid4(),parsed_jcs=body,sources=sources,read_document=no_io,
        resolve_dependency=no_io,publish_proposal=no_io,publish_document=publish)
    report=json.loads(result.report_jcs)
    assert (report['entry_count'],report['rejected_count'],report['unsupported_count'])==(4,1,3)
    assert result.proposals==()
    assert len(json.loads(result.shard_bodies[0])['entries'])==4


def adopted_fixture():
    from uuid import uuid5
    from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage
    from photo_worker.v2.interchange_metadata import MetadataReceipts
    body,_=fixture()
    parsed=json.loads(body)
    workflow,project,version=uuid4(),uuid4(),uuid4()
    image=WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(),sha256='d'*64),
        canvas=Canvas(width=32,height=24,orientation=1,space_id='canonical',source_sha256='a'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='e'*64,encoding='gamma24'),
        dtype='float32',alpha=None,transform_chain=TransformChain.create([]))
    image_raw=canonical_bytes(image)
    master={'artifact_id':str(uuid4()),'sha256':sha256(image_raw).hexdigest()}
    entries=[{'ordinal':i,'entry_id':e['entry_id'],'state':'adopted' if i==0 else 'unsupported',
        'reason':None if i==0 else 'unsupported_image_format','asset_version_id':str(version) if i==0 else None,
        'master':master if i==0 else None} for i,e in enumerate(parsed['manifest']['entries'])]
    run=uuid5(workflow,'interchange:canonical-ingest:1')
    sources=ImportSources.model_validate_json(canonical_bytes({'schema_version':1,'workflow_id':str(workflow),'project_id':str(project),
        'parsed':{'artifact_id':str(uuid4()),'sha256':sha256(body).hexdigest()},'ingest_run_id':str(run),'entries':entries}))
    receipts=MetadataReceipts.model_validate_json(canonical_bytes({'schema_version':1,'workflow_id':str(workflow),'project_id':str(project),
        'parsed':sources.parsed.model_dump(mode='json'),'collection_id':str(uuid5(workflow,'interchange:draft-collection:1')),
        'entries':[{'entry_id':entries[0]['entry_id'],'asset_version_id':str(version),'master':master,
            'item_id':str(uuid5(run,'item:0')),'rating':0,'flag':'none'}]}))
    return body,sources,receipts,image_raw


def test_adopted_stage_uses_actual_metadata_receipts():
    body,sources,receipts,image_raw=adopted_fixture()
    def no_proposal(*args):
        raise AssertionError('recipe absent: no proposal expected')
    result=prepare_import_stage(tenant_id=uuid4(),parsed_jcs=body,sources=sources,
        read_document=lambda *args:image_raw,resolve_dependency=no_proposal,publish_proposal=no_proposal,
        publish_document=lambda raw,*args:ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest()),
        metadata_receipts=receipts)
    first=json.loads(result.shard_bodies[0])['entries'][0]
    fields={f['name']:f['status'] for f in first['fields']}
    assert fields=={'image':'exact','rating':'exact','pick':'exact','crop':'exact','recipe':'unsupported'}
    assert first['asset_version_id']==str(sources.entries[0].asset_version_id)
    assert first['proposed_document'] is None


def test_document_reader_cannot_mutate_frozen_master_identity():
    body, sources, receipts, image_raw = adopted_fixture()
    expected = sources.entries[0].master.model_copy(deep=True)
    def read(ref, limit):
        assert ref == expected and limit == 1 << 20
        ref.artifact_id = uuid4()
        ref.sha256 = 'f' * 64
        return image_raw
    def no_proposal(*args):
        raise AssertionError('recipe absent: no proposal expected')
    result = prepare_import_stage(tenant_id=uuid4(), parsed_jcs=body, sources=sources,
        read_document=read, resolve_dependency=no_proposal, publish_proposal=no_proposal,
        publish_document=lambda raw,*args:ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest()),
        metadata_receipts=receipts)
    assert sources.entries[0].master == expected
    first = json.loads(result.shard_bodies[0])['entries'][0]
    assert first['fields'][0]['status'] == 'exact'


def test_proposal_publisher_cannot_rebind_compiled_output():
    import pytest
    from photo_worker.v2.adoption import AdoptionInvalid
    from photo_worker.v2.interchange_recipe import ImportedRecipeProposal
    from photo_worker.v2.render_recipe import EditingRecipeV3
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(b'proposal').hexdigest())
    proposal = ImportedRecipeProposal(ref, EditingRecipeV3.model_construct(), b'proposal')
    def mutate_and_return(published):
        published.artifact.artifact_id = uuid4()
        return published.artifact
    with pytest.raises(AdoptionInvalid, match='PROPOSAL_PUBLICATION'):
        publish_import_proposal(proposal, mutate_and_return)
    assert proposal.artifact == ref
    passed = []
    def publish(published):
        passed.append(published)
        return published.artifact.model_copy(deep=True)
    frozen = publish_import_proposal(proposal, publish)
    passed[0].artifact.artifact_id = uuid4()
    assert frozen == ref == proposal.artifact


def test_adopted_stage_without_receipts_fails_before_object_io():
    import pytest
    from photo_worker.v2.adoption import AdoptionInvalid
    body,sources,_,_=adopted_fixture()
    def no_io(*args):
        pytest.fail('unexpected IO')
    with pytest.raises(AdoptionInvalid,match='RECEIPTS_REQUIRED'):
        prepare_import_stage(tenant_id=uuid4(),parsed_jcs=body,sources=sources,read_document=no_io,
            resolve_dependency=no_io,publish_proposal=no_io,publish_document=no_io)


def test_pinned_xmp_unsupported_properties_reach_published_mapping_report():
    from pathlib import PurePosixPath
    from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp, CRS
    body, sources, receipts, image_raw = adopted_fixture()
    parsed = json.loads(body)
    image_entry = parsed['manifest']['entries'][0]
    sidecar_path = str(PurePosixPath(image_entry['image_path']).with_suffix('.xmp'))
    xmp = export_xmp(XMPMapping(0, 'unmarked', 1, None, ()))
    xmp = xmp.replace(b'rdf:about=""', b'rdf:about="" crs:Exposure2012="2"')
    xmp_sha = sha256(xmp).hexdigest()
    xmp_ref = ArtifactRef(artifact_id=uuid4(), sha256=xmp_sha)
    parsed['manifest']['inventory'].append({'artifact_id':str(uuid4()), 'path':sidecar_path,
        'sha256':xmp_sha, 'contract':'XMP'})
    parsed['files'].append({'path':sidecar_path, 'content':xmp_ref.model_dump(mode='json'), 'contract':'XMP'})
    body = canonical_bytes(parsed)
    sources.parsed.sha256 = sha256(body).hexdigest()
    receipts.parsed.sha256 = sources.parsed.sha256
    reads = []

    def read(ref, limit):
        reads.append(ref.model_copy(deep=True))
        if ref == xmp_ref:
            assert limit == 1 << 20
            return xmp
        assert ref == sources.entries[0].master
        return image_raw

    def no_proposal(*args):
        raise AssertionError('no recipe expected')

    result = prepare_import_stage(tenant_id=uuid4(), parsed_jcs=body, sources=sources,
        read_document=read, resolve_dependency=no_proposal, publish_proposal=no_proposal,
        publish_document=lambda raw,*args:ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest()),
        metadata_receipts=receipts)
    first = json.loads(result.shard_bodies[0])['entries'][0]
    field = next(field for field in first['fields'] if field['name'] == 'xmp_properties')
    digest = sha256(canonical_bytes([CRS + '}Exposure2012'])).hexdigest()
    assert field['status'] == 'unsupported'
    assert field['reason'] == 'unsupported_xmp_properties:1:' + digest
    assert reads == [sources.entries[0].master, xmp_ref]
    assert json.loads(result.report_jcs)['unsupported_count'] == 4
