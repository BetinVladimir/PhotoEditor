"""Version-pinned V28 before/after evidence for the native encoded validator."""
from .adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from .adoption_store import _Location
from .multi_preview_reader import PostgresMultiPreviewReader


class PostgresRestorationPreviewReader(PostgresMultiPreviewReader):
    def __init__(self, connection, objects, *, tenant_id, run_id, task_id,
                 result_id, side, variant, attempt, fence, allowed_bucket):
        if side not in ('before', 'after'):
            raise ValueError('Invalid restoration side')
        super().__init__(connection, objects, tenant_id=tenant_id, run_id=run_id,
                         task_id=task_id, result_id=result_id, variant=variant,
                         attempt=attempt, fence=fence, allowed_bucket=allowed_bucket)
        self.side = side

    def staged_outputs(self):
        rows = self._read('SELECT task_id,result_id,attempt,fence,artifact_id,sha256 '
            'FROM photo.v28_restoration_preview_result(%s,%s,%s,%s)',
            (self.tenant_id, self.run_id, self.side, self.variant))
        if len(rows) != 2 or any(row[:4] != (self.task_id, self.result_id,
                                           self.attempt, self.fence) for row in rows):
            raise AdoptionInvalid('RESTORATION_PREVIEW_RESULT_STALE')
        refs = {ArtifactRef(row[4], row[5]) for row in rows}
        if len(refs) != 2:
            raise AdoptionInvalid('RESTORATION_PREVIEW_RESULT_STALE')
        return refs

    def _location(self, ref):
        self._require_idle()
        self.staged_outputs()
        rows = self._read('SELECT * FROM photo.v28_restoration_preview_evidence'
            '(%s,%s,%s,%s,%s,%s,%s,%s)',
            (self.tenant_id, self.run_id, self.side, self.variant,
             self.result_id, self.attempt, self.fence, ref.artifact_id))
        if len(rows) != 1:
            raise AdoptionInvalid('RESTORATION_PREVIEW_EVIDENCE_INVALID')
        row = rows[0]
        if (row[0] != self.allowed_bucket or row[3] != ref.sha256
                or type(row[4]) is not int or row[4] < 1
                or not isinstance(row[1], str) or not row[1]
                or not isinstance(row[2], str) or row[2] in ('', 'null')):
            raise AdoptionInvalid('RESTORATION_PREVIEW_EVIDENCE_INVALID')
        location = _Location(row[0], row[1], row[2],
            ArtifactEvidence(ref, row[5], row[4], row[6], row[7]))
        prior = self._locations.get(ref)
        if prior is not None and prior != location:
            raise AdoptionInvalid('RESTORATION_PREVIEW_LOCATOR_CHANGED')
        self._locations[ref] = location
        return location

    def verified_locations(self):
        self._require_idle()
        if len(self._locations) != 4:
            raise AdoptionInvalid('RESTORATION_PREVIEW_LOCATOR_INVENTORY_INVALID')
        return super().verified_locations()
