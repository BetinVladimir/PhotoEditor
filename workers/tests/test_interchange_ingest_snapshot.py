import json
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactEvidence
from photo_worker.v2.interchange_ingest_snapshot import prepare_imported_ingest_snapshot


def fixture():
    entries, files, inventory, sizes = [], [], [], {}
    for index, name in enumerate(['one.NEF','unsupported.psd','large.tiff','last.jpg']):
        identifier = uuid4()
        ref = {'artifact_id':str(identifier),'sha256':'a'*64}
        entries.append({'entry_id':str(uuid4()),'external_id':str(index),'image_path':name,
            'image_sha256':'a'*64,'recipe_path':None,'recipe_sha256':None,'rating':0,'pick':'unmarked','crop':None})
        files.append({'path':name,'content':ref,'contract':'original'})
        inventory.append({'artifact_id':str(uuid4()),'path':name,'sha256':'a'*64,'contract':'original'})
        sizes[identifier] = 536870913 if index == 2 else 1024
    body = canonical_bytes({'schema_version':1,'manifest':{'schema_version':1,'renderer_version':'v2.2',
        'entries':entries,'masks':[],'inventory':inventory},'files':files})
    reader = SimpleNamespace(evidence=lambda ref:ArtifactEvidence(ref,'application/octet-stream',sizes[ref.artifact_id],'imported_file','output'))
    return body, reader


def test_snapshot_retains_original_ordinals_and_quality_settings():
    body, reader = fixture()
    raw = prepare_imported_ingest_snapshot(body, reader)
    items = json.loads(raw)
    assert [item['ordinal'] for item in items] == [0,3]
    assert [item['payload']['suffix'] for item in items] == ['.nef','.jpg']
    assert all(item['payload']['assume_profile'] is None and item['payload']['analyze'] is False
        and item['payload']['tile'] == 512 for item in items)
    assert raw == canonical_bytes(items)


def test_snapshot_rejects_untrusted_source_evidence():
    body, reader = fixture()
    evidence = reader.evidence
    reader.evidence = lambda ref: ArtifactEvidence(ref,'application/octet-stream',evidence(ref).bytes,'imported_file','input')
    with pytest.raises(AdoptionInvalid, match='EVIDENCE_INVALID'):
        prepare_imported_ingest_snapshot(body,reader)


def test_snapshot_rejects_file_contract_changed_after_parse():
    body, reader = fixture()
    parsed = json.loads(body)
    parsed['files'][0]['contract'] = 'DifferentContract'
    with pytest.raises(AdoptionInvalid, match='INGEST_INVENTORY'):
        prepare_imported_ingest_snapshot(canonical_bytes(parsed), reader)


def test_ingest_snapshot_stops_when_evidence_callback_loses_lease():
    from photo_worker.v2.interchange_ingest_snapshot import prepare_imported_ingest_snapshot
    from photo_worker.v2.adoption import ArtifactEvidence
    import pytest
    body, _ = fixture()
    cancelled = False
    calls = []

    def active():
        if cancelled:
            raise RuntimeError('lease lost')

    class Reader:
        def evidence(self, reference):
            nonlocal cancelled
            calls.append(reference)
            cancelled = True
            return ArtifactEvidence(reference, 'application/octet-stream', 100, 'imported_file', 'output')

    with pytest.raises(RuntimeError, match='lease lost'):
        prepare_imported_ingest_snapshot(body, Reader(), check_active=active)
    assert len(calls) == 1
