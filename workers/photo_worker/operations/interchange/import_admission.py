"""SQL current-attempt and frozen-source authority for the leased import adapter."""
from hashlib import sha256
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.interchange_plan import InterchangeImportParams
from .import_adapter import ImportAuthority


class SQLImportAdmission:
    def __init__(self, database):
        if not callable(getattr(database, 'interchange_import_context', None)):
            raise ValueError('INTERCHANGE_ADMISSION_UNAVAILABLE')
        self.database = database

    def admit(self, ctx, params):
        ctx.check_active()
        command = ctx.lease.command
        if (not isinstance(params, InterchangeImportParams) or params.evidence is None
                or command.operation != 'v2.interchange.import' or command.operation_version != '1'
                or command.resource_class != 'cpu'):
            raise OperationFailure('INTERCHANGE_SOURCE_STALE')
        raw = canonical_bytes(params)
        if len(raw) > 65536 or sha256(raw).hexdigest() != command.params.sha256:
            raise OperationFailure('INPUT_INVALID')
        result = self.database.interchange_import_context(ctx.lease, raw)
        ctx.check_active()
        if not isinstance(result, dict) or set(result) != {
                'workflow_id', 'project_id', 'snapshot_digest', 'max_input_json_bytes', 'max_output_bytes'}:
            raise OperationFailure('INTERCHANGE_SOURCE_STALE')
        try:
            workflow, project = UUID(str(result['workflow_id'])), UUID(str(result['project_id']))
            snapshot = result['snapshot_digest']
            inputs, outputs = result['max_input_json_bytes'], result['max_output_bytes']
            if (workflow.int == 0 or project.int == 0 or not isinstance(snapshot, str) or len(snapshot) != 64
                    or any(c not in '0123456789abcdef' for c in snapshot)
                    or type(inputs) is not int or not 1 <= inputs <= 128 << 20
                    or type(outputs) is not int or not 1 <= outputs <= 2**53-1):
                raise ValueError('invalid SQL import authority')
        except (ValueError, TypeError, AttributeError) as error:
            raise OperationFailure('INTERCHANGE_SOURCE_STALE') from error
        return ImportAuthority(workflow, project, snapshot, inputs, outputs)
