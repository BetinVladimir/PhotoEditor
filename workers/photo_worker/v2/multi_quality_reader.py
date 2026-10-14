"""Current scheduler-scoped master quality evidence, with no object IO in SQL TX."""
from psycopg.pq import TransactionStatus
from .adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef
from .adoption_store import PostgresObjectEvidenceReader, _Location


class PostgresMultiQualityReader(PostgresObjectEvidenceReader):
    def __init__(self, connection, objects, *, tenant_id, run_id, task_id,
                 result_id, attempt, fence, allowed_bucket):
        super().__init__(connection, objects, tenant_id=tenant_id, task_id=task_id,
                         attempt=attempt, fence=fence, allowed_bucket=allowed_bucket)
        if run_id.int == 0 or result_id.int == 0:
            raise ValueError('Invalid multi quality scope')
        self.run_id, self.result_id = run_id, result_id

    def _require_idle(self):
        info = getattr(self.connection, 'info', None)
        if (not self.connection.autocommit or info is not None
                and info.transaction_status != TransactionStatus.IDLE):
            raise AdoptionInvalid('MULTI_QUALITY_READ_TRANSACTION_ACTIVE')

    def _read(self, sql, args):
        self._require_idle()
        with self.connection.transaction():
            if self.connection.execute('SELECT current_user').fetchone() != ('photo_scheduler',):
                raise AdoptionInvalid('MULTI_QUALITY_READER_ROLE_INVALID')
            self.connection.execute("SELECT set_config('app.tenant_id',%s,true)",
                                    (str(self.tenant_id),))
            self.connection.execute("SET LOCAL statement_timeout='15s'")
            rows = self.connection.execute(sql, args).fetchall()
        self._require_idle()
        return rows

    def staged_outputs(self):
        rows = self._read('SELECT task_id,result_id,attempt,fence,artifact_id,sha256 '
            'FROM photo.v29_multi_quality_result(%s,%s)',
            (self.tenant_id, self.run_id))
        if len(rows) != 1 or any(row[:4] != (self.task_id, self.result_id,
                                           self.attempt, self.fence) for row in rows):
            raise AdoptionInvalid('MULTI_QUALITY_RESULT_STALE')
        refs = {ArtifactRef(row[4], row[5]) for row in rows}
        if len(refs) != 1:
            raise AdoptionInvalid('MULTI_QUALITY_RESULT_STALE')
        return refs

    def _location(self, ref):
        self._require_idle()
        self.staged_outputs()
        rows = self._read('SELECT * FROM photo.v29_multi_quality_evidence'
            '(%s,%s,%s,%s,%s,%s)',
            (self.tenant_id, self.run_id, self.result_id,
             self.attempt, self.fence, ref.artifact_id))
        if len(rows) != 1:
            raise AdoptionInvalid('MULTI_QUALITY_EVIDENCE_INVALID')
        row = rows[0]
        if (row[0] != self.allowed_bucket or row[3] != ref.sha256
                or type(row[4]) is not int or row[4] < 1
                or not isinstance(row[1], str) or not row[1]
                or not isinstance(row[2], str) or row[2] in ('', 'null')):
            raise AdoptionInvalid('MULTI_QUALITY_EVIDENCE_INVALID')
        location = _Location(row[0], row[1], row[2],
            ArtifactEvidence(ref, row[5], row[4], row[6], row[7]))
        prior = self._locations.get(ref)
        if prior is not None and prior != location:
            raise AdoptionInvalid('MULTI_QUALITY_LOCATOR_CHANGED')
        self._locations[ref] = location
        return location

    def read_json(self, ref, max_bytes):
        self._require_idle()
        raw = super().read_json(ref, max_bytes)
        self._require_idle()
        self._location(ref)
        return raw

    def iter_bytes(self, ref, max_bytes):
        iterator = super().iter_bytes(ref, max_bytes)
        try:
            while True:
                self._require_idle()
                try:
                    block = next(iterator)
                except StopIteration:
                    self._location(ref)
                    return
                yield block
        finally:
            iterator.close()

    def verified_locations(self):
        """Owned locator witnesses for locked SQL comparison after report validation."""
        self._require_idle()
        if len(self._locations) != 2:
            raise AdoptionInvalid('MULTI_QUALITY_LOCATOR_INVENTORY_INVALID')
        return [{'artifact_id': str(ref.artifact_id), 'sha256': ref.sha256,
                 'bucket': loc.bucket, 'key': loc.key, 'version': loc.version,
                 'bytes': loc.evidence.bytes, 'media_type': loc.evidence.media_type,
                 'role': loc.evidence.role, 'source': loc.evidence.source}
                for ref, loc in sorted(self._locations.items(),
                                       key=lambda item: str(item[0].artifact_id))]
