from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.interchange_export_result import publish_export_result
from photo_worker.v2.interchange_import_result import InterchangeResult
from photo_worker.v2.interchange_mapping import MappingEntry, MappingField
from test_interchange_export_prepare import fixture


def setup():
    plan, sources, _ = fixture()
    bundle = ArtifactRef(artifact_id=uuid4(), sha256=sha256(b'zip').hexdigest())
    entry = plan.entries[0]
    mappings = (MappingEntry(entry_id=entry.entry_id, external_id=entry.external_id,
        asset_version_id=entry.binding.asset_version_id, proposed_document=None,
        fields=[MappingField(name='rating', status='exact', reason=None)]),)
    return plan, bundle, mappings, tuple(source.reference for source in sources)


def test_export_result_has_bundle_and_report_without_import_drafts():
    plan, bundle, mappings, inputs = setup();published = []
    def publish(body, contract, ordinal):
        published.append((body, contract))
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    def root(document):
        body = canonical_bytes(document)
        document.bundle = None
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    result = publish_export_result(plan, bundle, bundle.sha256, mappings, inputs, publish, root)
    document = InterchangeResult.model_validate_json(result.document_jcs)
    assert document.bundle == bundle and document.report == result.report
    assert document.draft_shards == [] and document.imported_files == []
    assert [kind for _, kind in published] == ['MappingShard', 'MappingReport']
    assert result.output_json_bytes == sum(len(body) for body, _ in published) + len(result.document_jcs)
    assert result.root.sha256 == sha256(result.document_jcs).hexdigest()


@pytest.mark.parametrize('mode', ['bundle_hash', 'bundle_alias', 'mapping_identity', 'proposal', 'quota'])
def test_export_result_invalid_input_never_publishes_root(mode):
    plan, bundle, mappings, inputs = setup();root_calls = [];published = []
    digest = bundle.sha256
    if mode == 'bundle_hash': digest = 'a'*64
    elif mode == 'bundle_alias': bundle.artifact_id = inputs[0].artifact_id
    elif mode == 'mapping_identity': mappings[0].entry_id = uuid4()
    elif mode == 'proposal': mappings[0].proposed_document = inputs[0]
    def publish(body, *args):
        published.append(body)
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    with pytest.raises(ValueError):
        publish_export_result(plan, bundle, digest, mappings, inputs, publish,
            lambda document: root_calls.append(document), max_report_bytes=2 if mode == 'quota' else 8 << 20)
    assert root_calls == [] and published == []


@pytest.mark.parametrize('role', ['shard', 'report', 'root'])
def test_export_result_rejects_input_alias_at_every_publication(role):
    plan, bundle, mappings, inputs = setup()
    def publish(body, contract, ordinal):
        bad = role == 'shard' and contract == 'MappingShard' or role == 'report' and contract == 'MappingReport'
        return ArtifactRef(artifact_id=inputs[0].artifact_id if bad else uuid4(), sha256=sha256(body).hexdigest())
    def root(document):
        return ArtifactRef(artifact_id=inputs[0].artifact_id, sha256=sha256(canonical_bytes(document)).hexdigest())
    with pytest.raises(ValueError, match='RECEIPT'):
        publish_export_result(plan, bundle, bundle.sha256, mappings, inputs, publish, root)
