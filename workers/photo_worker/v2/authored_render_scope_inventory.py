"""Lease-bound primary inventory transport; physical receipt decoding is separate.

This reader never accepts caller-provided inventories. It is not a compute or
qualification factory and does not make unsupported schema versions executable.
"""
from .authored_render_live_scope import SQLAuthoredLiveScope
from photo_worker.runtime.executor import OperationFailure

READERS = {
 ('render.compose','2'): 'read_authored_render_final_worker_scope',
 ('render.authored_segment','1'): 'read_authored_render_segment_worker_scope',
 ('render.authored_geometry','1'): 'read_authored_render_geometry_worker_scope',
}

class SQLAuthoredScopeInventory:
    def __init__(self, database):
        self.live = SQLAuthoredLiveScope(database)
        self.database = self.live.database

    def read(self, ctx, raw):
        # Admit before indexing the closed operation map or accessing any rows.
        with self.live.current(ctx, raw):
            c=ctx.lease.command; lease=ctx.lease
            name=READERS[(c.operation,c.operation_version)]
            row=self.database._query('SELECT photo.'+name+'(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                (c.tenant_id,c.run_id,c.task_id,c.attempt,lease.fence,lease.worker_id,
                 c.params.artifact_id,c.params.sha256,raw.decode('utf-8'),lease.input_digest))
            fields={'context','base_receipt','base_receipt_jcs','phases'}
            geometry=(c.operation=='render.authored_geometry')
            if geometry:fields|={'parallel_source','parallel_source_jcs','source_jcs'}
            if (type(row) not in (tuple,list) or len(row)!=1 or type(row[0]) is not dict
                    or set(row[0])!=fields
                    or type(row[0]['context']) is not dict
                    or type(row[0]['base_receipt']) is not dict
                    or type(row[0]['base_receipt_jcs']) is not str
                    or type(row[0]['phases']) is not list):
                raise OperationFailure('AUTHORED_SCOPE_INVENTORY_STALE')
            if geometry and (type(row[0]['parallel_source']) is not dict
                    or type(row[0]['parallel_source_jcs']) is not str
                    or type(row[0]['source_jcs']) is not str):
                raise OperationFailure('AUTHORED_SCOPE_INVENTORY_STALE')
            inventory=row[0]
        # No inventory escapes if the second live source/pair check failed.
        return inventory
