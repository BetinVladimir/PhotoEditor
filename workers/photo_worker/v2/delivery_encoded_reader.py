"""Delivery encode object reader with fresh authority and immutable location pins."""
from uuid import UUID
from .adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from .adoption_store import PostgresObjectEvidenceReader, _Location


class DeliveryEncodeEvidenceReader(PostgresObjectEvidenceReader):
    def __init__(self, connection, objects, *, result_id, **scope):
        if not isinstance(result_id, UUID) or result_id.int == 0:
            raise ValueError('DELIVERY_ENCODE_RESULT_INVALID')
        super().__init__(connection, objects, **scope)
        if self._exposure:
            raise ValueError('DELIVERY_ENCODE_SCOPE_INVALID')
        self.result_id = result_id

    def _location(self, ref):
        # Never serve authority from a cache. The first location is only a pin
        # against replacement; every access rereads SQL under this result fence.
        row = self.connection.execute(
            'SELECT bucket,object_key,object_version,sha256,bytes,media_type,role,source '
            'FROM photo.v30_delivery_encode_artifact(%s,%s,%s,%s,%s,%s)',
            (self.tenant_id, self.task_id, self.attempt, self.fence, self.result_id, ref.artifact_id),
        ).fetchone()
        if (row is None or row[0] != self.allowed_bucket or row[3] != ref.sha256
                or type(row[4]) is not int or row[4] < 1
                or not isinstance(row[1], str) or not row[1]
                or not isinstance(row[2], str) or row[2] in ('', 'null')
                or row[7] not in ('input', 'output')):
            raise AdoptionInvalid('DELIVERY_ENCODE_ARTIFACT_EVIDENCE_INVALID')
        current = _Location(row[0], row[1], row[2], ArtifactEvidence(ref, row[5], row[4], row[6], row[7]))
        previous = self._locations.get(ref)
        if previous is not None and previous != current:
            raise AdoptionInvalid('DELIVERY_ENCODE_OBJECT_CHANGED')
        self._locations[ref] = current
        return current

    def staged_outputs(self):
        rows = self.connection.execute(
            'SELECT artifact_id,sha256,state FROM photo.v30_delivery_encode_staged_outputs(%s,%s,%s,%s,%s)',
            (self.tenant_id, self.task_id, self.attempt, self.fence, self.result_id),
        ).fetchall()
        if len(rows) != 2 or any(state != 'ready' for _, _, state in rows):
            raise AdoptionInvalid('DELIVERY_ENCODE_STAGED_OUTPUTS_INVALID')
        refs = {ArtifactRef.parse({'artifact_id': str(artifact), 'sha256': digest}) for artifact, digest, _ in rows}
        if len(refs) != 2:
            raise AdoptionInvalid('DELIVERY_ENCODE_STAGED_OUTPUTS_INVALID')
        return refs
