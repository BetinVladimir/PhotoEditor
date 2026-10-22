from sqlite3 import connect
from uuid import UUID, uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.input_closure import V2InputIndex, V2InputsInvalid

class Reservation:
    def release(self):
        pass

def index_fixture():
    conn = connect(':memory:')
    conn.execute('CREATE TABLE allowed (artifact_id TEXT PRIMARY KEY,sha TEXT NOT NULL,contract TEXT,is_entry INTEGER NOT NULL)')
    entries = [(str(uuid4()), 'a'*64, 'StyleV2', 1), (str(uuid4()), 'b'*64, 'MaskRef', 1)]
    conn.executemany('INSERT INTO allowed VALUES(?,?,?,?)', entries)
    conn.execute('INSERT INTO allowed VALUES(?,?,?,?)', (str(uuid4()), 'c'*64, None, 0))
    return V2InputIndex(conn, Reservation()), entries

def test_declared_entries_excludes_transport_and_owns_typed_rows():
    index, rows = index_fixture()
    try:
        first = index.declared_entries(2)
        assert {(str(item.ref.artifact_id),item.ref.sha256,item.contract) for item in first} == {row[:3] for row in rows}
        first[0].contract = 'WrongContract'
        first[0].ref.sha256 = '0'*64
        assert all(item.contract != 'WrongContract' and item.ref.sha256 != '0'*64 for item in index.declared_entries(2))
        with pytest.raises(V2InputsInvalid):
            index.declared_entries(1)
    finally:
        index.close()
    with pytest.raises(V2InputsInvalid):
        index.declared_entries()

@pytest.mark.parametrize('limit', [0, -1, 30001, True, 1.5, None])
def test_declared_entries_rejects_invalid_limit(limit):
    index, _ = index_fixture()
    try:
        with pytest.raises(V2InputsInvalid):
            index.declared_entries(limit)
    finally:
        index.close()


def test_exact_typed_entry_rejects_wrong_sha_and_contract():
    index, rows = index_fixture()
    try:
        ref = ArtifactRef(artifact_id=UUID(rows[0][0]), sha256=rows[0][1])
        index.require_entry(ref, rows[0][2])
        with pytest.raises(V2InputsInvalid, match='TASK_INPUT_CONTRACT_MISMATCH'):
            index.require_entry(ref, 'NpyArray')
        changed = ref.model_copy(update={'sha256': 'f'*64})
        with pytest.raises(V2InputsInvalid, match='TASK_INPUT_CONTRACT_MISMATCH'):
            index.require_entry(changed, rows[0][2])
        absent = ArtifactRef(artifact_id=uuid4(), sha256='e'*64)
        with pytest.raises(V2InputsInvalid, match='TASK_INPUT_CONTRACT_MISMATCH'):
            index.require_entry(absent, rows[0][2])
    finally:
        index.close()
