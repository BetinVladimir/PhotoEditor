"""Pure registry packaging; legacy typed names are never executable schema refs.

Baseline describes route/service types. V2 is the executable schema projection.
The packaged current registry is their canonical union, with V2 authoritative
for an exact identity. Neither arbitrary mirrors nor guessed DTO aliases enter
this projection.
"""
from copy import deepcopy
import re


def _identity(row):
    method, path = row.get('method'), row.get('path')
    if (method not in ('GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS')
            or not isinstance(path, str) or not path.startswith('/')
            or '?' in path or '#' in path):
        raise ValueError('ENDPOINT_IDENTITY_INVALID')
    return method, path


def _index(rows):
    indexed, normalized = {}, {}
    for row in rows:
        key = _identity(row)
        shape = key[0], re.sub(r'\{[^{}]+\}', '{}', key[1])
        if shape in normalized and normalized[shape] != key:
            raise ValueError('ENDPOINT_PARAMETER_ALIAS_CONFLICT')
        normalized[shape] = key
        if key in indexed and indexed[key] != row:
            raise ValueError('ENDPOINT_DUPLICATE_CONFLICT')
        indexed[key] = deepcopy(row)
    return indexed


def package_endpoints(baseline, executable, schemas):
    if baseline.get('format_version') != 1 or executable.get('schema_version') != 1:
        raise ValueError('ENDPOINT_REGISTRY_FORMAT_INVALID')
    legacy = _index(baseline['endpoints'])
    current = _index(executable['endpoints'])
    # Detect spelling aliases across the two sources as well. They cannot be
    # merged by guessing which path parameter's caller/API name was intended.
    aliases = {}
    for key in (*legacy, *current):
        normalized = key[0], re.sub(r'\{[^{}]+\}', '{}', key[1])
        if normalized in aliases and aliases[normalized] != key:
            raise ValueError('ENDPOINT_PARAMETER_ALIAS_CONFLICT')
        aliases[normalized] = key
    packaged = {}
    for key, row in legacy.items():
        # Preserve native names/metadata. A typed documentation symbol is not a
        # JSON Schema merely because its spelling happens to match a $def.
        packaged[key] = {
            'method': key[0], 'path': key[1], 'handler': row['handler'],
            'role': row['authorization'], 'package': 'V1',
            'success_status': row['status'],
            'idempotency': row.get('idempotency', False),
            'revision_required': row.get('revision_required', False),
            'request_schema': None, 'response_schema': None,
            'origin': ['baseline'], 'contract_format': 'legacy_typed_source',
            'typed_source_contract': row,
        }
        if 'description' in row:
            packaged[key]['description'] = row['description']
        if 'additional_statuses' in row:
            packaged[key]['additional_statuses'] = deepcopy(row['additional_statuses'])
    definitions = schemas.get('$defs', {})
    for key, row in current.items():
        for field in ('request_schema', 'response_schema', 'query_schema'):
            name = row.get(field)
            if name is not None and (not isinstance(name, str) or name not in definitions):
                raise ValueError('ENDPOINT_EXECUTABLE_SCHEMA_UNRESOLVED')
        projected = deepcopy(row)
        projected['origin'] = ['baseline', 'v2'] if key in legacy else ['v2']
        projected['contract_format'] = 'executable_schema'
        if key in legacy:
            projected['typed_source_contract'] = legacy[key]
        packaged[key] = projected
    return {'schema_version': 1,
            'description': 'Current canonical route union. Only executable_schema entries reference schemas.json; legacy_typed_source entries retain native service/type declarations without claiming executable DTO coverage.',
            'endpoints': [packaged[key] for key in sorted(packaged)]}
