"""Scheduler authority bracket around immutable object reconstruction."""
from copy import deepcopy
from hashlib import sha256
from uuid import UUID, uuid5
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .adoption import ArtifactRef as EvidenceRef, AdoptionInvalid, require_adoption_active
from .interchange_import_adoption import verify_import_output


def prepare_current_interchange_import(connection, reader, tenant_id, task_id,
        context, *, qualify_recipe=None, check_active=lambda: None):
    if not callable(check_active):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_CONTEXT')
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    ctx = deepcopy(context)
    if (not isinstance(tenant_id, UUID) or tenant_id.int == 0
            or not isinstance(task_id, UUID) or task_id.int == 0
            or ctx.operation != 'v2.interchange.import' or ctx.version != '1'
            or type(ctx.attempt) is not int or not 1 <= ctx.attempt <= 2**31-1
            or type(ctx.fence) is not int or not 1 <= ctx.fence <= 2**63-1
            or not isinstance(ctx.result_id, UUID) or ctx.result_id.int == 0):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_CONTEXT')
    if not connection.autocommit:
        raise ValueError('Import preparation requires an autocommit read connection')
    values = (tenant_id, task_id, ctx.attempt, ctx.fence, ctx.result_id)

    def current():
        check_active()
        row = connection.execute('SELECT photo.v30_interchange_import_adoption_context(%s,%s,%s,%s,%s)', values).fetchone()
        check_active()
        if row is None or not isinstance(row[0], dict) or set(row[0]) != {
                'params', 'workflow_id', 'project_id', 'snapshot_digest', 'max_output_bytes'}:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_STALE')
        value = deepcopy(row[0])
        try:
            params = ArtifactRef.model_validate_json(canonical_bytes(value['params']))
            workflow, project = UUID(str(value['workflow_id'])), UUID(str(value['project_id']))
            digest, budget = value['snapshot_digest'], value['max_output_bytes']
            if (workflow.int == 0 or project.int == 0 or params.artifact_id.int == 0
                    or task_id != uuid5(workflow, 'v2.interchange.import:0')
                    or not isinstance(digest, str) or len(digest) != 64
                    or any(c not in '0123456789abcdef' for c in digest)
                    or type(budget) is not int or not 1 <= budget <= 2**53-1):
                raise ValueError('invalid import authority')
        except (ValueError, TypeError, AttributeError) as error:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_AUTHORITY') from error
        return value, params, workflow, project

    authority, params, workflow, project = current()
    def read(ref, maximum, source):
        return read_import_evidence(reader, ref, maximum, source, check_active=check_active)
    contracts = {'document': 'InterchangeResult', 'report': 'MappingReport', 'mapping_shard': 'MappingShard'}
    entries = []
    for pin in reader.staged_outputs():
        check_active()
        evidence = reader.evidence(pin)
        if evidence.ref != pin or evidence.source != 'output' or evidence.role not in contracts:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_ROLE')
        if len(entries) >= 52:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INVENTORY')
        entries.append((ArtifactRef(artifact_id=pin.artifact_id, sha256=pin.sha256), contracts[evidence.role]))
    proof = verify_import_output(root=ArtifactRef(artifact_id=ctx.root.artifact_id, sha256=ctx.root.sha256),
        staged_entries=entries, params_ref=params, params_jcs=read(params, 65536, 'input'),
        task_id=task_id, workflow_id=workflow, project_id=project,
        snapshot_digest=authority['snapshot_digest'], max_output_bytes=authority['max_output_bytes'],
        read_input=lambda ref, maximum: read(ref, maximum, 'input'),
        read_output=lambda ref, maximum: read(ref, maximum, 'output'),
        qualify_recipe=qualify_recipe, check_active=check_active)
    if current()[0] != authority:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_AUTHORITY_CHANGED')
    check_active()
    return proof


def read_import_evidence(reader, ref, maximum, source, *, check_active=lambda: None):
    """Only input opaque leaves may use bounded binary reads; outputs are JSON."""
    if not callable(check_active):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_EVIDENCE')
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    check_active()
    if type(maximum) is not int or not 2 <= maximum <= 8 << 20 or source not in ('input', 'output'):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_EVIDENCE')
    pin = EvidenceRef(ref.artifact_id, ref.sha256)
    evidence = reader.evidence(pin)
    check_active()
    if evidence.ref != pin or evidence.source != source or type(evidence.bytes) is not int or not 2 <= evidence.bytes <= maximum:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_EVIDENCE')
    if evidence.media_type == 'application/json':
        raw = reader.read_json(pin, maximum)
    elif source == 'input' and evidence.media_type == 'application/octet-stream' and maximum <= 1 << 20:
        iterator = getattr(reader, 'iter_bytes', None)
        if not callable(iterator):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_EVIDENCE')
        stream = iterator(pin, maximum)
        data = bytearray()
        try:
            for block in stream:
                check_active()
                if not isinstance(block, bytes) or not block or len(block) > evidence.bytes - len(data):
                    raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_EVIDENCE')
                data.extend(block)
        finally:
            close = getattr(stream, 'close', None)
            if callable(close):
                close()
        raw = bytes(data)
    else:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_EVIDENCE')
    check_active()
    if not isinstance(raw, bytes) or len(raw) != evidence.bytes or sha256(raw).hexdigest() != ref.sha256:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_HASH')
    return raw
