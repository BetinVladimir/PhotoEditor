from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_mapping import MappingEntry,publish_mapping_report
from photo_worker.v2.interchange_import_result import publish_import_result
from photo_worker.v2.interchange_parse import ParsedInterchange
from test_interchange_ingest_snapshot import fixture


def test_final_import_root_preserves_full_parsed_file_closure():
    raw,_=fixture();parsed=ParsedInterchange.model_validate_json(raw)
    entries=[MappingEntry(entry_id=e.entry_id,external_id=e.external_id,asset_version_id=None,proposed_document=None,fields=[]) for e in parsed.manifest.entries]
    def publish(body,*args):return ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    mapping=publish_mapping_report(entries,[(e.entry_id,e.external_id) for e in entries],publish)
    result=publish_import_result(parsed,mapping,lambda document:publish(canonical_bytes(document)))
    assert result.document.bundle is None
    assert result.document.imported_files==[file.content for file in parsed.files]
    assert result.document.draft_shards==list(mapping.shards)
    assert result.root.sha256==sha256(result.document_jcs).hexdigest()
    with pytest.raises(AdoptionInvalid,match='ROOT_PUBLICATION'):
        publish_import_result(parsed,mapping,lambda document:parsed.files[0].content)


def test_root_publisher_cannot_mutate_retained_import_document():
    raw, _ = fixture()
    parsed = ParsedInterchange.model_validate_json(raw)
    entries = [MappingEntry(entry_id=e.entry_id, external_id=e.external_id,
        asset_version_id=None, proposed_document=None, fields=[]) for e in parsed.manifest.entries]
    def publish(body, *args):
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    mapping = publish_mapping_report(entries,
        [(e.entry_id, e.external_id) for e in entries], publish)
    def mutate(document):
        original = canonical_bytes(document)
        document.imported_files.clear()
        document.report.artifact_id = uuid4()
        return publish(original)
    result = publish_import_result(parsed, mapping, mutate)
    assert result.document.imported_files == [file.content for file in parsed.files]
    assert result.document.report == mapping.report
    assert canonical_bytes(result.document) == result.document_jcs


def test_import_root_rejects_proposals_absent_from_mapping_shards():
    from dataclasses import replace
    raw, _ = fixture()
    parsed = ParsedInterchange.model_validate_json(raw)
    entries = [MappingEntry(entry_id=e.entry_id, external_id=e.external_id,
        asset_version_id=None, proposed_document=None, fields=[]) for e in parsed.manifest.entries]
    def publish(body, *args):
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    mapping = publish_mapping_report(entries,
        [(e.entry_id, e.external_id) for e in entries], publish)
    forged = replace(mapping, proposals=(ArtifactRef(artifact_id=uuid4(), sha256='a' * 64),))
    with pytest.raises(AdoptionInvalid, match='REPORT_CONTENT'):
        publish_import_result(parsed, forged, lambda document:pytest.fail('root published'))


def test_import_root_rejects_noncanonical_mapping_report():
    from dataclasses import replace
    raw, _ = fixture()
    parsed = ParsedInterchange.model_validate_json(raw)
    entries = [MappingEntry(entry_id=e.entry_id, external_id=e.external_id,
        asset_version_id=None, proposed_document=None, fields=[]) for e in parsed.manifest.entries]
    def publish(body, *args):
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    mapping = publish_mapping_report(entries,
        [(e.entry_id, e.external_id) for e in entries], publish)
    noncanonical = b' ' + mapping.report_jcs
    forged = replace(mapping, report_jcs=noncanonical,
        report=ArtifactRef(artifact_id=mapping.report.artifact_id, sha256=sha256(noncanonical).hexdigest()))
    with pytest.raises(AdoptionInvalid, match='REPORT_MISMATCH'):
        publish_import_result(parsed, forged, lambda document:pytest.fail('root published'))


@pytest.mark.parametrize('change', ['contract', 'digest', 'extra', 'casefold_collision'])
def test_import_root_rejects_parsed_files_outside_manifest(change):
    raw, _ = fixture()
    parsed = ParsedInterchange.model_validate_json(raw)
    entries = [MappingEntry(entry_id=e.entry_id, external_id=e.external_id,
        asset_version_id=None, proposed_document=None, fields=[]) for e in parsed.manifest.entries]
    def publish(body, *args):
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    mapping = publish_mapping_report(entries,
        [(e.entry_id, e.external_id) for e in entries], publish)
    if change == 'contract':
        parsed.files[0].contract = 'OtherContract'
    elif change == 'digest':
        parsed.files[0].content.sha256 = 'f' * 64
    elif change == 'extra':
        extra = parsed.files[0].model_copy(deep=True)
        extra.path = 'extra.bin'
        extra.content.artifact_id = uuid4()
        parsed.files.append(extra)
    else:
        parsed.manifest.inventory[1].path = parsed.manifest.inventory[0].path.upper()
        parsed.files[1].path = parsed.manifest.inventory[1].path
    with pytest.raises(AdoptionInvalid, match='PARSED_INVENTORY'):
        publish_import_result(parsed, mapping, lambda document:pytest.fail('root published'))
