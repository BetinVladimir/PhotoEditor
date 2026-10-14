"""Dedicated SQL arguments combine semantic reconstruction and closure proof."""
from hashlib import sha256
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes, parse_json
from .adoption import AdoptionInvalid, PreparedAdoption
from .interchange_import_adoption import VerifiedImportOutput


def interchange_import_commit_statement(tenant_id, task_id, context, verified, closure):
    if (not isinstance(tenant_id, UUID) or tenant_id.int == 0
            or not isinstance(task_id, UUID) or task_id.int == 0
            or context.operation != 'v2.interchange.import' or context.version != '1'
            or type(context.attempt) is not int or not 1 <= context.attempt <= 2**31-1
            or type(context.fence) is not int or not 1 <= context.fence <= 2**63-1
            or not isinstance(context.result_id, UUID) or context.result_id.int == 0
            or not isinstance(verified, VerifiedImportOutput) or not isinstance(closure, PreparedAdoption)
            or verified.root_id != context.root.artifact_id or verified.root_sha256 != context.root.sha256
            or sha256(verified.root_jcs).hexdigest() != verified.root_sha256
            or sha256(verified.params_jcs).hexdigest() != verified.params_sha256
            or sha256(canonical_bytes(list(closure.inventory))).hexdigest() != closure.digest):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_COMMIT_CONTEXT')
    receipts = parse_json(verified.proposal_closures_jcs, max_bytes=128 << 20)
    if (not isinstance(receipts, list) or canonical_bytes(receipts) != verified.proposal_closures_jcs
            or tuple((UUID(item['recipe']['artifact_id']), item['recipe']['sha256']) for item in receipts) != verified.proposal_pins):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_COMMIT_RECIPE_CLOSURES')
    from psycopg.types.json import Jsonb
    return ('SELECT photo.adopt_v30_interchange_import(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
        (tenant_id, task_id, context.attempt, context.fence, context.result_id,
         Jsonb(list(closure.inventory)), closure.digest, verified.params_jcs.decode('utf-8'),
         verified.root_jcs.decode('utf-8'), verified.report_jcs.decode('utf-8'),
         Jsonb([raw.decode('utf-8') for raw in verified.shard_bodies]), Jsonb(receipts)))
