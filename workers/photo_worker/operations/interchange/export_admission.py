"""Strict worker client for independently fenced SQL export authority."""
from copy import deepcopy
from hashlib import sha256
from uuid import UUID, uuid5

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.interchange_plan import InterchangeExportParams
from .export_adapter import ExportAuthority


class SQLExportAdmission:
    def __init__(self, database):
        if not callable(getattr(database, 'interchange_export_context', None)):
            raise ValueError('INTERCHANGE_EXPORT_ADMISSION_UNAVAILABLE')
        self.database = database

    def admit(self, ctx, params):
        if not isinstance(params, InterchangeExportParams):
            raise OperationFailure('INPUT_INVALID')
        raw = canonical_bytes(params)
        ctx.check_active()
        command = ctx.lease.command
        if (command.operation != 'v2.interchange.export' or command.operation_version != '1'
                or command.resource_class != 'cpu' or not isinstance(command.task_id, UUID)
                or command.task_id.int == 0 or len(raw) > 65536
                or command.params.artifact_id != uuid5(command.task_id, 'interchange:params:1')
                or sha256(raw).hexdigest() != command.params.sha256):
            raise OperationFailure('INTERCHANGE_SOURCE_STALE')
        result = deepcopy(self.database.interchange_export_context(ctx.lease, raw))
        ctx.check_active()
        fields = {'workflow_id', 'project_id', 'snapshot_digest', 'max_archive_bytes',
                  'max_output_bytes', 'max_input_json_bytes'}
        if not isinstance(result, dict) or set(result) != fields:
            raise OperationFailure('INTERCHANGE_SOURCE_STALE')
        try:
            workflow, project = UUID(str(result['workflow_id'])), UUID(str(result['project_id']))
            snapshot = result['snapshot_digest']
            archive, output, inputs = (result[name] for name in
                ('max_archive_bytes', 'max_output_bytes', 'max_input_json_bytes'))
            if (workflow.int == 0 or project.int == 0 or not isinstance(snapshot, str)
                    or len(snapshot) != 64 or any(c not in '0123456789abcdef' for c in snapshot)
                    or type(archive) is not int or not 1 <= archive <= 64 << 30
                    or type(output) is not int or not archive <= output <= 2**53-1
                    or type(inputs) is not int or not 1 <= inputs <= 128 << 20):
                raise ValueError('invalid export authority')
        except (ValueError, TypeError, AttributeError) as error:
            raise OperationFailure('INTERCHANGE_SOURCE_STALE') from error
        return ExportAuthority(workflow, project, snapshot, archive, output, inputs)
