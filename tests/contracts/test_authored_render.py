"""Actual Go compiler DTO fixtures; no worker or SQL admission evidence."""
import copy
import json
from pathlib import Path
from uuid import UUID

import pytest

from photo_worker.contracts.authored_render import decode_authored_params
from photo_worker.contracts.base import canonical_bytes

FIXTURES = Path(__file__).parent / 'fixtures' / 'authored_render'


@pytest.mark.parametrize('name,kind', [('final','final'), ('segment','segment'),
    ('segment_null_declaration','segment'), ('geometry','geometry'), ('geometry_upscale','geometry')])
def test_real_go_compiler_canonical_roundtrip(name, kind):
    raw = (FIXTURES / (name + '.json')).read_bytes()
    assert canonical_bytes(decode_authored_params(raw, kind)) == raw


def check_bad(kind, edit):
    d = json.loads((FIXTURES / (kind + '.json')).read_bytes())
    edit(d)
    with pytest.raises(ValueError):
        decode_authored_params(json.dumps(d).encode(), kind)


@pytest.mark.parametrize('kind', ['final', 'segment', 'geometry'])
@pytest.mark.parametrize('field,value', [('tenant_id', str(UUID(int=0))),
    ('schema_version', True), ('program_sha256', 'A'*64), ('extra', 1)])
def test_closed_common(kind, field, value):
    check_bad(kind, lambda d: d.__setitem__(field, value))


def test_segment_count_and_required_nulls():
    check_bad('segment', lambda d: d.__setitem__('ordinal', 2))
    check_bad('segment', lambda d: d.pop('current_layers'))
    check_bad('segment', lambda d: d['recipe'].__setitem__('artifact_id', str(UUID(int=0))))


def test_final_count_and_alias():
    check_bad('final', lambda d: d.__setitem__('phase_receipts', []))
    check_bad('final', lambda d: d['phase_receipts'].append(copy.deepcopy(d['phase_receipts'][0])))
    check_bad('final', lambda d: d.__setitem__('phase_receipts', [d['recipe']]))


@pytest.mark.parametrize('mutation', [
    lambda d: d.__setitem__('instruction_id', str(UUID(int=99))),
    lambda d: d['native']['task'].__setitem__('producer_run_id', d['parent_run_id']),
    lambda d: d['native']['task'].__setitem__('operation_version', '2'),
    lambda d: d['native']['task'].__setitem__('execution_profile_jcs', '{ "backend":"cpu"}'),
    lambda d: d['native']['task'].__setitem__('execution_profile_sha256', '0'*64),
    lambda d: d['native']['task'].__setitem__('outputs', [d['native']['image']]),
    lambda d: d['native']['task']['outputs'].append(copy.deepcopy(d['native']['task']['outputs'][0])),
    lambda d: d['native']['report'].__setitem__('artifact_id', d['native']['image']['artifact_id']),
    lambda d: d['native']['task']['outputs'][0].__setitem__('sha256', 'f'*64),
    lambda d: d['native']['task'].__setitem__('operation', 'image.upscale'),
])
def test_geometry_native_role_and_provenance_checks(mutation):
    check_bad('geometry', mutation)


def test_duplicate_keys_refused_before_model():
    raw = (FIXTURES / 'final.json').read_bytes()
    with pytest.raises(ValueError):
        decode_authored_params(b'{"schema_version":1,' + raw[1:], 'final')


@pytest.mark.parametrize('kind', ['final', 'segment', 'geometry'])
def test_required_references_and_ordered_phase_limits(kind):
    check_bad(kind, lambda d: d['source'].__setitem__('asset_version_id', str(UUID(int=0))))
    check_bad(kind, lambda d: d.__setitem__('phase_receipts', [d['recipe']] * 4999))


def test_geometry_derived_run_profile_and_phase_role_refusal():
    from uuid import uuid5
    check_bad('geometry', lambda d: d['native']['task'].__setitem__(
        'producer_run_id', str(uuid5(UUID(d['instruction_id']), 'AuthoredGeometryRun/v1'))))
    check_bad('geometry', lambda d: d['native']['task'].__setitem__('execution_profile_jcs', '{}'))
    check_bad('geometry', lambda d: d['native']['task'].__setitem__('attempt', True))


def test_unknown_kind_and_nonfinite_refused():
    with pytest.raises(ValueError):
        decode_authored_params(b'{}', 'other')
    with pytest.raises(ValueError):
        decode_authored_params(b'{"value":NaN}', 'final')
