"""Arguments for atomic provenance metadata and artifact-link publication."""
from hashlib import sha256
from uuid import UUID
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid
from .provenance_adoption import PreparedSigningAdoption


def signing_commit_statement(tenant_id, task_id, context, prepared):
    if (not isinstance(tenant_id, UUID) or tenant_id.int == 0
            or not isinstance(task_id, UUID) or task_id.int == 0
            or context.operation != 'provenance.sign' or context.version != '1'
            or type(context.attempt) is not int or not 1 <= context.attempt <= 2**31-1
            or type(context.fence) is not int or not 1 <= context.fence <= 2**63-1
            or not isinstance(context.result_id, UUID) or context.result_id.int == 0
            or not isinstance(prepared, PreparedSigningAdoption)):
        raise AdoptionInvalid('SIGNING_COMMIT_CONTEXT_INVALID')
    verified = prepared.verified
    inventory = prepared.adoption.inventory
    if (not isinstance(inventory, tuple) or len(inventory) != 3
            or any(not isinstance(item, dict) for item in inventory)
            or sha256(canonical_bytes(list(inventory))).hexdigest() != prepared.adoption.digest):
        raise AdoptionInvalid('SIGNING_COMMIT_INVENTORY_CHANGED')
    owned_inventory = [dict(item) for item in inventory]
    return ('SELECT photo.adopt_v31_signed(' + ','.join(['%s']*12) + ')',
        (tenant_id, task_id, context.attempt, context.fence, context.result_id,
         Jsonb(owned_inventory), prepared.adoption.digest,
         verified.params_jcs.decode('utf-8'), verified.result_jcs.decode('utf-8'),
         verified.policy_jcs.decode('utf-8'), verified.lineage_jcs.decode('utf-8'),
         verified.validation_sha))
