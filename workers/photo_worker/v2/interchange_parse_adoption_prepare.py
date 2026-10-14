"""Fenced scheduler preparation; no object IO inside the final commit TX."""
from copy import deepcopy
from uuid import UUID

from .adoption import AdoptionInvalid, ArtifactRef, require_adoption_active
from .interchange_parse_adoption import prepare_parsed_interchange_adoption


def prepare_current_interchange_parse(connection, reader, tenant_id, task_id,
                                      context, scratch, *, check_active=lambda: None):
    """Authorize both sides of reconstruction using current SQL authority.

    This does not adopt outputs. The eventual commit must independently recheck
    the same attempt, fence, result, current ACL and exact reconstructed bytes.
    """
    ctx = deepcopy(context)
    if (not isinstance(tenant_id, UUID) or tenant_id.int == 0
            or not isinstance(task_id, UUID) or task_id.int == 0
            or ctx.operation != 'v2.interchange.parse' or ctx.version != '1'
            or type(ctx.attempt) is not int or not 1 <= ctx.attempt <= 2**31-1
            or type(ctx.fence) is not int or not 1 <= ctx.fence <= 2**63-1
            or not isinstance(ctx.result_id, UUID) or ctx.result_id.int == 0
            or not callable(check_active)):
        raise AdoptionInvalid('INTERCHANGE_ADOPTION_CONTEXT_INVALID')
    if not connection.autocommit:
        raise ValueError('Interchange preparation requires an autocommit read connection')
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)

    values = (tenant_id, task_id, ctx.attempt, ctx.fence, ctx.result_id)

    def current_params():
        check_active()
        row = connection.execute(
            'SELECT * FROM photo.v30_interchange_parse_adoption_params(%s,%s,%s,%s,%s)',
            values).fetchone()
        if row is None:
            raise AdoptionInvalid('INTERCHANGE_ADOPTION_PARAMS_STALE')
        reference = ArtifactRef(row[0], row[1])
        if (not isinstance(reference.artifact_id, UUID) or reference.artifact_id.int == 0
                or not isinstance(reference.sha256, str) or len(reference.sha256) != 64
                or any(c not in '0123456789abcdef' for c in reference.sha256)):
            raise AdoptionInvalid('INTERCHANGE_ADOPTION_PARAMS_INVALID')
        check_active()
        return reference

    params = current_params()
    prepared = prepare_parsed_interchange_adoption(task_id=task_id, reader=reader,
        params_ref=params, root=ctx.root, scratch=scratch, check_active=check_active)
    if current_params() != params:
        raise AdoptionInvalid('INTERCHANGE_ADOPTION_PARAMS_CHANGED')
    check_active()
    return prepared
