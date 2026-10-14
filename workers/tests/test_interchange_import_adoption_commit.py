"""Dedicated commit boundary tests; SQL execution deferred."""
from dataclasses import replace
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid, PreparedAdoption
from photo_worker.v2.interchange_import_adoption import verify_import_output
from photo_worker.v2.interchange_import_adoption_commit import interchange_import_commit_statement
from test_interchange_import_adoption import adoption_fixture


def fixture():
    args, _ = adoption_fixture()
    verified = verify_import_output(**args)
    ctx = SimpleNamespace(operation='v2.interchange.import', version='1', attempt=1,
        fence=4, result_id=uuid4(), root=ArtifactRef(verified.root_id, verified.root_sha256))
    inventory = ({'artifact_id': str(verified.root_id), 'sha256': verified.root_sha256,
                  'purpose': 'a'*64, 'ordinal': 0},)
    closure = PreparedAdoption(inventory, sha256(canonical_bytes(list(inventory))).hexdigest())
    return uuid4(), args['task_id'], ctx, verified, closure


def test_commit_binds_exact_context_owned_document_bytes_and_closure_digest():
    tenant, task, ctx, proof, closure = fixture()
    statement, values = interchange_import_commit_statement(tenant, task, ctx, proof, closure)
    assert statement.startswith('SELECT photo.adopt_v30_interchange_import(')
    assert statement.count('%s') == len(values) == 12
    assert values[:5] == (tenant, task, 1, 4, ctx.result_id)
    assert values[6] == closure.digest
    assert values[7:10] == (proof.params_jcs.decode(), proof.root_jcs.decode(), proof.report_jcs.decode())
    assert values[10].obj == [raw.decode() for raw in proof.shard_bodies]
    assert values[11].obj == []


@pytest.mark.parametrize('change', ['operation', 'attempt', 'fence', 'root', 'params', 'digest'])
def test_commit_rejects_mismatched_proof_before_sql(change):
    tenant, task, ctx, proof, closure = fixture()
    if change == 'operation':
        ctx.operation = 'v2.interchange.parse'
    elif change == 'attempt':
        ctx.attempt = True
    elif change == 'fence':
        ctx.fence = 0
    elif change == 'root':
        proof = replace(proof, root_id=uuid4())
    elif change == 'params':
        proof = replace(proof, params_jcs=b'{}')
    else:
        closure = replace(closure, digest='0'*64)
    with pytest.raises(AdoptionInvalid):
        interchange_import_commit_statement(tenant, task, ctx, proof, closure)


def test_commit_refuses_recipe_receipts_not_matching_verified_proposals():
    tenant, task, ctx, proof, closure = fixture()
    proof = replace(proof, proposal_closures_jcs=canonical_bytes([
        dict(recipe=dict(artifact_id=str(uuid4()), sha256='a'*64), refs=[])]))
    with pytest.raises(AdoptionInvalid, match='RECIPE_CLOSURES'):
        interchange_import_commit_statement(tenant, task, ctx, proof, closure)
