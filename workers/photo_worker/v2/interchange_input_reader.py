"""Declared-input evidence with immutable gateway metadata pins for V30."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef as WireRef
from photo_worker.runtime.gateway import ArtifactMetadata
from .adoption import ArtifactEvidence, ArtifactRef as EvidenceRef, AdoptionInvalid


class PreparedImportInputReader:
    def __init__(self, artifacts, *, max_bytes=128 << 30, check_active=lambda: None, read_budget=None):
        if (type(max_bytes) is not int or not 1 <= max_bytes <= 128 << 30
                or not callable(check_active)):
            raise ValueError('INTERCHANGE_READER_INSTALLATION')
        from .interchange_read_budget import ImportReadBudget
        if read_budget is not None and not isinstance(read_budget, ImportReadBudget):
            raise ValueError('INTERCHANGE_READER_INSTALLATION')
        self.read_budget = read_budget
        self.artifacts, self.active, self.maximum = artifacts, check_active, max_bytes
        self.entries, self.pins = {}, {}
        self.consumed = 0
        identities = {}
        for entry in artifacts.declared_input_entries(30000):
            key = (entry.ref.artifact_id, entry.ref.sha256)
            identity = (entry.ref.sha256, entry.contract)
            previous = identities.setdefault(entry.ref.artifact_id, identity)
            if entry.ref.artifact_id.int == 0 or previous != identity:
                raise ValueError('INTERCHANGE_READER_IDENTITY_CONFLICT')
            if key in self.entries:
                raise ValueError('INTERCHANGE_READER_DUPLICATE')
            self.entries[key] = entry.model_copy(deep=True)

    def _wire(self, reference):
        key = (reference.artifact_id, reference.sha256)
        if key not in self.entries:
            raise AdoptionInvalid('INTERCHANGE_READER_UNDECLARED')
        return WireRef(artifact_id=key[0], sha256=key[1])

    def _metadata(self, reference):
        wire = self._wire(reference)
        self.active()
        metadata = self.artifacts.input_metadata(wire)
        if (not isinstance(metadata, ArtifactMetadata) or type(metadata.bytes) is not int
                or not 1 <= metadata.bytes <= self.maximum):
            raise AdoptionInvalid('INTERCHANGE_READER_METADATA')
        key = (wire.artifact_id, wire.sha256)
        previous = self.pins.setdefault(key, metadata)
        if metadata != previous:
            raise AdoptionInvalid('INTERCHANGE_READER_VERSION_CHANGED')
        self.active()
        return wire, metadata

    def evidence(self, reference):
        wire, metadata = self._metadata(reference)
        return ArtifactEvidence(EvidenceRef(wire.artifact_id, wire.sha256), metadata.media_type, metadata.bytes, None, 'input')

    def iter_bytes(self, reference, maximum):
        if type(maximum) is not int or not 1 <= maximum <= self.maximum:
            raise AdoptionInvalid('INTERCHANGE_READER_BUDGET')
        wire, metadata = self._metadata(reference)
        if metadata.bytes > maximum or metadata.bytes > self.maximum - self.consumed:
            raise AdoptionInvalid('INTERCHANGE_READER_BUDGET')
        is_json = metadata.media_type == 'application/json'
        if self.read_budget is not None and (
                metadata.bytes > self.read_budget.remaining_input_bytes
                or is_json and metadata.bytes > self.read_budget.remaining_json_bytes):
            raise AdoptionInvalid('INTERCHANGE_READ_BUDGET')
        count, checksum = 0, sha256()
        with self.artifacts.stream(wire) as stream:
            if stream.metadata != metadata or stream.size != metadata.bytes:
                raise AdoptionInvalid('INTERCHANGE_READER_VERSION_CHANGED')
            while True:
                self.active()
                block = stream.read(min(1 << 20, metadata.bytes + 1 - count))
                if not block:
                    break
                if not isinstance(block, bytes) or count + len(block) > metadata.bytes:
                    raise AdoptionInvalid('INTERCHANGE_READER_STREAM')
                if self.read_budget is not None:
                    self.read_budget.consume(len(block), is_json=is_json)
                count += len(block)
                self.consumed += len(block)
                checksum.update(block)
                yield block
        if count != metadata.bytes or checksum.hexdigest() != wire.sha256:
            raise AdoptionInvalid('INTERCHANGE_READER_HASH')
        self._metadata(wire)
        self.active()

    def recheck(self):
        """Repeat every observed input pin after complete closure qualification."""
        for identifier, digest in tuple(self.pins):
            self._metadata(WireRef(artifact_id=identifier, sha256=digest))
        self.active()

    def read_json(self, reference, max_bytes):
        if type(max_bytes) is not int or not 2 <= max_bytes <= 8 << 20:
            raise AdoptionInvalid('INTERCHANGE_READER_JSON_BUDGET')
        if self.evidence(reference).media_type != 'application/json':
            raise AdoptionInvalid('INTERCHANGE_READER_JSON_MEDIA')
        return b''.join(self.iter_bytes(reference, max_bytes))
