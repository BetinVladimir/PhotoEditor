"""Bounded tenant-scoped V28 derivative receipt sweep."""
from uuid import UUID

from .adoption import AdoptionInvalid


class RestorationPreviewRunner:
    def __init__(self, coordinator):
        self.coordinator = coordinator
        self.after_tenant = None
        self.tenant = None
        self.after_task = None
        self.after_completion_run = None

    def run_once(self):
        counts = {'adopted': 0, 'rejected': 0, 'finalized': 0, 'retry': 0}
        c = self.coordinator
        c.check_active()
        if self.tenant is None:
            rows = c._query(c.read, '',
                'SELECT * FROM photo.next_v28_restoration_preview_receipt_tenant(%s)',
                (self.after_tenant,))
            if not rows:
                self.after_tenant = None
                return counts
            if (len(rows) != 1 or len(rows[0]) != 1 or
                    not isinstance(rows[0][0], UUID) or rows[0][0].int == 0 or
                    self.after_tenant is not None and rows[0][0].int <= self.after_tenant.int):
                raise AdoptionInvalid('RESTORATION_PREVIEW_TENANT_INVALID')
            self.tenant = rows[0][0]
        tenant = self.tenant
        rows = c._query(c.read, tenant,
            'SELECT * FROM photo.pending_v28_restoration_preview_receipts(%s,%s)',
            (tenant, self.after_task))
        if len(rows) > 32:
            raise AdoptionInvalid('RESTORATION_PREVIEW_PENDING_INVALID')
        previous = self.after_task
        seen = set()
        for item in rows:
            if not isinstance(item, (tuple, list)) or len(item) != 4:
                raise AdoptionInvalid('RESTORATION_PREVIEW_PENDING_INVALID')
            run, side, variant, task = item
            if (not isinstance(run, UUID) or run.int == 0 or
                    not isinstance(task, UUID) or task.int == 0 or
                    side not in ('before', 'after') or
                    variant not in ('preview1600', 'preview2560') or
                    task in seen or previous is not None and task.int <= previous.int):
                raise AdoptionInvalid('RESTORATION_PREVIEW_PENDING_INVALID')
            previous = task
            seen.add(task)
        for run, side, variant, task in rows:
            c.check_active()
            try:
                if c.adopt(tenant, run, side, variant):
                    counts['adopted'] += 1
                else:
                    counts['rejected'] += 1
            except InterruptedError:
                raise
            except Exception:
                counts['retry'] += 1
        completion = c._query(c.read, tenant,
            'SELECT * FROM photo.pending_v28_restoration_preview_completion(%s,%s)',
            (tenant, self.after_completion_run))
        if len(completion) > 32:
            raise AdoptionInvalid('RESTORATION_PREVIEW_COMPLETION_INVALID')
        last_run = self.after_completion_run
        for item in completion:
            if (not isinstance(item, (tuple, list)) or len(item) != 1 or
                    not isinstance(item[0], UUID) or item[0].int == 0 or
                    last_run is not None and item[0].int <= last_run.int):
                raise AdoptionInvalid('RESTORATION_PREVIEW_COMPLETION_INVALID')
            last_run = item[0]
        for (run,) in completion:
            c.check_active()
            try:
                row = c._query(c.write, tenant,
                    'SELECT photo.finalize_successful_v28_restoration_preview(%s,%s)',
                    (tenant, run), one=True)
                if not isinstance(row, tuple) or len(row) != 1 or type(row[0]) is not bool:
                    raise AdoptionInvalid('RESTORATION_PREVIEW_FINALIZER_INVALID')
                counts['finalized'] += int(row[0])
            except InterruptedError:
                raise
            except Exception:
                counts['retry'] += 1
        self.after_task = previous if len(rows) == 32 else None
        self.after_completion_run = last_run if len(completion) == 32 else None
        if self.after_task is None and self.after_completion_run is None:
            self.after_tenant = tenant
            self.tenant = None
        return counts
