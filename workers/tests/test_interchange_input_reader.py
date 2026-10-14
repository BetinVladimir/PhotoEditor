from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.gateway import ArtifactMetadata
from photo_worker.v2.input_closure import InputClosureEntry
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_input_reader import PreparedImportInputReader

class Stream(BytesIO):
    def __init__(self, body, metadata):
        super().__init__(body);self.metadata=metadata;self.size=len(body)

class Artifacts:
    def __init__(self):
        self.body=b'{"schema_version":1}'
        self.ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(self.body).hexdigest())
        self.metadata=ArtifactMetadata(len(self.body),'application/json','a'*64)
        self.opens=0;self.reads=0;self.change=False;self.stream_change=False
    def declared_input_entries(self, maximum):
        return (InputClosureEntry(ref=self.ref,contract='Params'),)
    def input_metadata(self, ref):
        self.reads+=1
        if self.change and self.reads>1:return ArtifactMetadata(len(self.body),'application/json','b'*64)
        return self.metadata
    def stream(self, ref):
        self.opens+=1
        metadata=self.metadata if not self.stream_change else ArtifactMetadata(len(self.body),'application/json','b'*64)
        return Stream(self.body,metadata)

@pytest.mark.parametrize('mode',['valid','version','stream_version','hash','undeclared','budget','media'])
def test_prepared_input_reader_pins_declared_identity_and_version(mode):
    artifacts=Artifacts();reader=PreparedImportInputReader(artifacts)
    reference=artifacts.ref.model_copy(deep=True);maximum=1024
    if mode=='version':artifacts.change=True
    if mode=='stream_version':artifacts.stream_change=True
    if mode=='hash':artifacts.body=b'x'+artifacts.body[1:]
    if mode=='undeclared':reference.artifact_id=uuid4()
    if mode=='budget':maximum=2
    if mode=='media':artifacts.metadata=ArtifactMetadata(len(artifacts.body),'application/octet-stream','a'*64)
    if mode=='valid':
        assert reader.read_json(reference,maximum)==artifacts.body
        assert artifacts.opens==1 and artifacts.reads>=3
    else:
        with pytest.raises(AdoptionInvalid):reader.read_json(reference,maximum)
        if mode in ('version','undeclared','budget','media'):assert artifacts.opens==0


def test_input_evidence_owns_reference_before_active_callback():
    artifacts=Artifacts()
    reference=artifacts.ref.model_copy(deep=True)
    expected=(reference.artifact_id,reference.sha256)
    def mutate():
        reference.artifact_id=uuid4()
        reference.sha256='0'*64
    reader=PreparedImportInputReader(artifacts,check_active=mutate)
    evidence=reader.evidence(reference)
    assert (evidence.ref.artifact_id,evidence.ref.sha256)==expected
    assert (reference.artifact_id,reference.sha256)!=expected


def test_prepared_input_reader_shared_json_budget_rejects_before_stream():
    from photo_worker.v2.interchange_read_budget import ImportReadBudget
    artifacts = Artifacts()
    budget = ImportReadBudget(len(artifacts.body))
    reader = PreparedImportInputReader(artifacts, read_budget=budget)
    assert reader.read_json(artifacts.ref, 1024) == artifacts.body
    assert budget.input_bytes == len(artifacts.body)
    with pytest.raises(AdoptionInvalid, match='READ_BUDGET'):
        reader.read_json(artifacts.ref, 1024)
    assert artifacts.opens == 1
    assert budget.remaining_json_bytes == 0


@pytest.mark.parametrize('mode', ['sha', 'contract', 'both', 'duplicate'])
def test_declared_input_identity_collisions_refuse_before_metadata_or_stream(mode):
    artifacts = Artifacts()
    first = InputClosureEntry(ref=artifacts.ref.model_copy(deep=True), contract='Params')
    second = first.model_copy(deep=True)
    if mode in ('sha', 'both'): second.ref.sha256 = 'f' * 64
    if mode in ('contract', 'both'): second.contract = 'MaskRef'
    artifacts.declared_input_entries = lambda maximum: (first, second)
    with pytest.raises(ValueError, match='IDENTITY_CONFLICT|DUPLICATE'):
        PreparedImportInputReader(artifacts)
    assert artifacts.opens == 0 and artifacts.reads == 0


def test_declared_input_reader_owns_identity_and_contract_after_installation():
    artifacts = Artifacts()
    declaration = InputClosureEntry(ref=artifacts.ref.model_copy(deep=True), contract='Params')
    artifacts.declared_input_entries = lambda maximum: (declaration,)
    reader = PreparedImportInputReader(artifacts)
    declaration.ref.sha256 = 'f' * 64
    declaration.contract = 'MaskRef'
    stored = reader.entries[(artifacts.ref.artifact_id, artifacts.ref.sha256)]
    assert stored.contract == 'Params' and stored.ref == artifacts.ref
    assert reader.read_json(artifacts.ref, 1024) == artifacts.body
