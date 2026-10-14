"""Bounded runtime sweep with independent constant-memory phase cursors."""
from uuid import UUID
from .adoption import AdoptionInvalid
from .perspective_preview_maintenance import PerspectivePreviewReceiptMaintenance


class PerspectivePreviewRunner:
    def __init__(self, coordinator):
        self.coordinator = coordinator
        self.maintenance = PerspectivePreviewReceiptMaintenance(coordinator)
        self.positions = {phase: [None, None, None] for phase in
                          ('lifecycle', 'receipts', 'completion')}

    def _tenant(self, after):
        c = self.coordinator
        # Discovery has no tenant data access; set empty transaction-local scope.
        rows = c._query(c.read, '',
            'SELECT * FROM photo.next_v29_perspective_preview_tenant(%s)', (after,))
        if not rows: return None
        if (len(rows) != 1 or len(rows[0]) != 1 or not isinstance(rows[0][0], UUID)
                or rows[0][0].int == 0 or after is not None and rows[0][0].int <= after.int):
            raise AdoptionInvalid('PERSPECTIVE_PREVIEW_TENANT_DISCOVERY_INVALID')
        return rows[0][0]

    def run_once(self):
        counts = {'adopted': 0, 'rejected': 0, 'finalized': 0, 'retry': 0,
                  'cancel_requested': 0, 'released': 0}
        def retry(_exc): counts['retry'] += 1
        for phase, position in self.positions.items():
            self.coordinator.check_active()
            after_tenant, tenant, cursor = position
            try:
                if tenant is None:
                    tenant = self._tenant(after_tenant)
                    if tenant is None:
                        position[:] = [None, None, None]
                        continue
                if phase == 'lifecycle':
                    next_cursor, changes = self.maintenance.process_lifecycle_page(tenant, cursor, on_retry=retry)
                    counts['cancel_requested'] += changes['cancel_requested']
                    counts['released'] += changes['released']
                    counts['finalized'] += changes['failed']
                elif phase == 'receipts':
                    next_cursor, accepted, rejected, completed = self.maintenance.process_page(tenant, cursor, on_retry=retry)
                    counts['adopted'] += accepted
                    counts['rejected'] += rejected
                    counts['finalized'] += completed
                else:
                    next_cursor, completed = self.maintenance.process_completion_page(tenant, cursor, on_retry=retry)
                    counts['finalized'] += completed
                position[:] = ([tenant, None, None] if next_cursor is None
                               else [after_tenant, tenant, next_cursor])
            except InterruptedError:
                raise
            except Exception:
                # Do not let one tenant block other tenants. Uncommitted work is
                # rediscovered next sweep; other phases retain their own cursor.
                counts['retry'] += 1
                position[:] = [tenant if tenant is not None else after_tenant, None, None]
        return counts
