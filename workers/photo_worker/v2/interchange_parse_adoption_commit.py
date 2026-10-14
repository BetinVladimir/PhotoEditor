"""Exact arguments for the dedicated atomic parsed-bundle commit."""
from uuid import UUID
from .adoption import AdoptionInvalid
from .interchange_parse_adoption import PreparedParsedAdoption


def interchange_parse_commit_statement(tenant_id, task_id, context, prepared):
    if (not isinstance(tenant_id, UUID) or tenant_id.int == 0
            or not isinstance(task_id, UUID) or task_id.int == 0
            or context.operation != 'v2.interchange.parse' or context.version != '1'
            or not isinstance(prepared, PreparedParsedAdoption)):
        raise AdoptionInvalid('INTERCHANGE_COMMIT_CONTEXT_INVALID')
    from psycopg.types.json import Jsonb
    return ('SELECT photo.adopt_v30_interchange_parsed_and_begin_ingest(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
        (tenant_id, task_id, context.attempt, context.fence, context.result_id,
         Jsonb(list(prepared.adoption.inventory)), prepared.adoption.digest,
         prepared.verified.params_jcs.decode('utf-8'),
         prepared.verified.result_jcs.decode('utf-8'),
         prepared.ingest_items_jcs.decode('utf-8')))
