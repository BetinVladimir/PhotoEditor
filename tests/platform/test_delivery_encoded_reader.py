from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid
from photo_worker.v2.delivery_encoded_reader import DeliveryEncodeEvidenceReader


def fixture():
    ref = ArtifactRef(uuid4(), 'a'*64)
    class Connection:
        autocommit = True
        row = ('photos', 'key', 'version-1', ref.sha256, 10, 'image/jpeg', 'encoded_image', 'output')
        rows = [(ref.artifact_id, ref.sha256, 'ready'), (uuid4(), 'b'*64, 'ready')]
        calls = []
        def execute(self, query, args):
            self.calls.append((query, args))
            return SimpleNamespace(fetchone=lambda: self.row, fetchall=lambda: self.rows)
    connection = Connection()
    reader = DeliveryEncodeEvidenceReader(connection, object(), tenant_id=uuid4(), task_id=uuid4(),
        attempt=3, fence=7, result_id=uuid4(), allowed_bucket='photos')
    return reader, connection, ref


@pytest.mark.parametrize('change', ['revoked', 'version', 'key', 'role', 'source', 'bytes'])
def test_delivery_reader_rechecks_and_pins_every_access(change):
    reader, connection, ref = fixture()
    assert reader.evidence(ref).ref == ref
    row = list(connection.row)
    index = {'version': 2, 'key': 1, 'role': 6, 'source': 7, 'bytes': 4}.get(change)
    if index is not None:
        row[index] = 11 if change == 'bytes' else 'changed'
    connection.row = None if change == 'revoked' else tuple(row)
    with pytest.raises(AdoptionInvalid):
        reader.evidence(ref)
    assert len(connection.calls) == 2
    assert connection.calls[0][1] == (reader.tenant_id, reader.task_id, 3, 7, reader.result_id, ref.artifact_id)


@pytest.mark.parametrize('change', ['valid', 'missing', 'extra', 'duplicate', 'not_ready'])
def test_delivery_reader_exact_staged_inventory(change):
    reader, connection, ref = fixture()
    if change == 'missing': connection.rows = []
    if change == 'extra': connection.rows.append((uuid4(), 'c'*64, 'ready'))
    if change == 'duplicate': connection.rows = [connection.rows[0]]*2
    if change == 'not_ready': connection.rows[0] = (ref.artifact_id, ref.sha256, 'uploading')
    if change == 'valid':
        assert len(reader.staged_outputs()) == 2
    else:
        with pytest.raises(AdoptionInvalid): reader.staged_outputs()
