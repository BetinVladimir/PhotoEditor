"""Physical registry parity; requires fresh isolated PostgreSQL migrations."""
import json
from pathlib import Path
import psycopg


def test_negative_registry_matches_normative_output_contract(database):
    root=Path(__file__).resolve().parents[2]
    op=next(x for x in json.loads((root/'docs/implementation-v2/contracts/operations.json').read_text())['operations'] if x['id']=='optical.negative')
    with psycopg.connect(**database) as connection:
        row=connection.execute("SELECT resource_class,routing_key,timeout_seconds,backends,v2_enabled FROM photo.operation_registry WHERE operation='optical.negative' AND version='1'").fetchone()
        assert row==('cpu','cpu.optical_negative',op['timeout_seconds'],['cpu'],False)
        rows=connection.execute("SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation='optical.negative' AND version='1'").fetchall()
        actual={role:(media,maximum,count) for role,media,maximum,count in rows}
        expected={role:(policy['media_types'],policy['max_bytes'],policy['max_count']) for role,policy in op['role_policies'].items()}
        assert actual==expected
