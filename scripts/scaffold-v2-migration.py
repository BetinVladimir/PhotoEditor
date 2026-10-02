#!/usr/bin/env python3
"""Extract one v2 package's table/constraint/RLS scaffold from the contract.

The output is deliberately not a migration: transactional functions, triggers,
GC, purge and role tests must be added before a goose file is published.
"""
from __future__ import annotations

import argparse
import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CONTRACT = ROOT / 'docs/implementation-v2/contracts/database.json'
REFERENCE = ROOT / 'docs/implementation-v2/contracts/schema-reference.sql'
TABLE_PATTERN = re.compile(r'\bphoto\.([a-z][a-z0-9_]*)\b')
SUPPORTED = ('CREATE TABLE ', 'ALTER TABLE ', 'CREATE INDEX ', 'CREATE POLICY ', 'GRANT ', 'REVOKE ')


def table_name(statement: str) -> str | None:
    if not statement.startswith(SUPPORTED):
        return None
    match = TABLE_PATTERN.search(statement)
    return match.group(1) if match else None


def schema_statements(migration: int) -> list[str]:
    contract = json.loads(CONTRACT.read_text())
    names = {table['name'] for table in contract['tables'] if table['migration'] == migration}
    if not names:
        raise ValueError(f'No tables assigned to migration {migration}')
    # schema-reference contains plain table/index/policy statements. It has no
    # function bodies or embedded semicolons; reject that assumption if changed.
    source = '\n'.join(line for line in REFERENCE.read_text().splitlines()
                       if not line.lstrip().startswith('--'))
    statements = [part.strip() for part in source.split(';') if part.strip()]
    selected = [statement + ';' for statement in statements if table_name(statement) in names]
    created = {table_name(statement) for statement in selected if statement.startswith('CREATE TABLE ')}
    if created != names:
        raise ValueError(f'Schema reference drift: expected {sorted(names)}, created {sorted(created)}')
    for statement in selected:
        if ' REFERENCES photo.' in statement:
            targets = re.findall(r'REFERENCES photo\.([a-z][a-z0-9_]*)', statement)
            if not targets:
                raise ValueError('Unparsed foreign key')
            for target in targets:
                candidate = next((table for table in contract['tables'] if table['name'] == target), None)
                if candidate is not None and candidate['migration'] > migration:
                    raise ValueError(f'Future migration dependency: {target}')
    return selected


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument('migration', type=int)
    args = parser.parse_args()
    print('-- Schema scaffold only; add functions, triggers, GC, purge and checks before migration.\n')
    print('\n\n'.join(schema_statements(args.migration)))


if __name__ == '__main__':
    main()
