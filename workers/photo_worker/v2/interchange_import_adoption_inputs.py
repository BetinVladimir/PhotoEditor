"""Current scheduler-only typed inputs for import recipe reconstruction."""

from dataclasses import dataclass
from uuid import UUID

from photo_worker.contracts.base import ArtifactRef as WireRef
from .adoption import AdoptionInvalid, ArtifactRef as EvidenceRef


@dataclass(frozen=True)
class ImportAdoptionInputInventory:
    entries: tuple[tuple[WireRef, str, str], ...]
    connection: object
    reader: object
    scope: tuple[UUID, UUID, int, int, UUID]

    def recheck(self):
        current = read_import_adoption_input_inventory(
            self.connection, self.reader, *self.scope)
        if current.entries != self.entries:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUTS_CHANGED')


def read_import_adoption_input_inventory(connection, reader, tenant_id, task_id,
                                         attempt, fence, result_id):
    if (not connection.autocommit or not isinstance(tenant_id, UUID) or tenant_id.int == 0
            or not isinstance(task_id, UUID) or task_id.int == 0
            or type(attempt) is not int or attempt < 1 or type(fence) is not int or fence < 1
            or not isinstance(result_id, UUID) or result_id.int == 0
            or not callable(getattr(reader, 'evidence', None))):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUTS_CONFIGURATION')
    scope = (tenant_id, task_id, attempt, fence, result_id)
    rows = connection.execute(
        'SELECT ordinal,artifact_id,sha256,contract,media_type '
        'FROM photo.v30_import_adoption_input_inventory(%s,%s,%s,%s,%s)',
        scope).fetchall()
    if not isinstance(rows, list) or not 2 <= len(rows) <= 30000:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUTS_STALE')
    entries, seen = [], set()
    for ordinal, row in enumerate(rows):
        if not isinstance(row, (tuple, list)) or len(row) != 5 or row[0] != ordinal:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUTS_ORDER')
        _, identifier, digest, contract, media_type = row
        if (not isinstance(identifier, UUID) or identifier.int == 0 or identifier in seen
                or not isinstance(digest, str) or len(digest) != 64
                or any(c not in '0123456789abcdef' for c in digest)
                or not isinstance(contract, str) or not 1 <= len(contract) <= 120
                or not isinstance(media_type, str) or not media_type):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUTS_IDENTITY')
        seen.add(identifier)
        ref = WireRef(artifact_id=identifier, sha256=digest)
        evidence = reader.evidence(EvidenceRef(identifier, digest))
        if (evidence.ref != EvidenceRef(identifier, digest)
                or evidence.source != 'input' or evidence.media_type != media_type
                or type(evidence.bytes) is not int or evidence.bytes < 2):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUTS_EVIDENCE')
        entries.append((ref, contract, media_type))
    return ImportAdoptionInputInventory(tuple(entries), connection, reader, scope)
