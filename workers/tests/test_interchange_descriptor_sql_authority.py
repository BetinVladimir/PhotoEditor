from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.interchange_descriptor_occurrences import SQLExportDescriptorAuthority
from test_interchange_descriptor_occurrences import fixture


@pytest.mark.parametrize('allowed', [True, False, 1, None])
def test_descriptor_sql_authority_binds_lease_and_owned_evidence(allowed):
    pin, event = fixture()
    pin.update(frame_binding=pin['selected_binding'], frame_ordinal=7)
    params = dict(schema_version=1, plan=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64), max_report_bytes=8388608)
    raw = canonical_bytes(params)
    command = SimpleNamespace(operation='v2.interchange.export', operation_version='1', resource_class='cpu',
        params=ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest()))
    lease = SimpleNamespace(command=command)
    calls = []
    ctx = SimpleNamespace(lease=lease, check_active=lambda: None)
    def check(actual_lease, actual_params, evidence):
        calls.append((actual_lease, actual_params, evidence))
        return allowed
    database = SimpleNamespace(interchange_export_descriptor_authority=check)
    authority = SQLExportDescriptorAuthority(database, ctx, params)
    assert authority(event, pin) is (allowed is True)
    assert calls[0][0] is lease and calls[0][1] == raw
    assert calls[0][2]['root_jcs'].encode() == pin['root_body']
    assert calls[0][2]['frame_ordinal'] == 7
    calls[0][2]['selected_binding']['canvas_hash'] = 'f' * 64
    assert pin['selected_binding'].canvas_hash == 'c' * 64


def test_descriptor_sql_authority_rejects_missing_database():
    with pytest.raises(ValueError, match='INSTALLATION'):
        SQLExportDescriptorAuthority(object(), None, None)
