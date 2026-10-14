"""Explicit tenant-scoped receipt and lifecycle work for the deployed adopter."""
from uuid import UUID
from .adoption import AdoptionInvalid


class PerspectiveQualityReceiptMaintenance:
    def __init__(self, coordinator):
        self.coordinator = coordinator

    def process_page(self, tenant_id, after=None, *, on_retry=None):
        """At most 32 decodes, no transaction retained across object reads.

        Return next task cursor and accepted/rejected/completed counts. On any
        authority/IO error the default caller retries the page; the runtime
        on_retry callback allows remaining rows to proceed and retries next sweep.
        Committed receipts are absent on replay. A short page ends the sweep (cursor None).
        """
        if not isinstance(tenant_id, UUID) or tenant_id.int == 0:
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_TENANT_INVALID')
        if after is not None and (not isinstance(after, UUID) or after.int == 0):
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_CURSOR_INVALID')
        c = self.coordinator
        rows = c._query(c.read, tenant_id,
            'SELECT * FROM photo.pending_v29_perspective_quality_receipts(%s,%s)',
            (tenant_id, after))
        if len(rows) > 32:
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
        # Validate the complete batch before any durable mutation.
        previous = after
        tasks = set()
        for item in rows:
            if not isinstance(item, (tuple, list)) or len(item) != 3:
                raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
            run, variant, task = item
            if (not isinstance(run, UUID) or run.int == 0
                    or not isinstance(task, UUID) or task.int == 0
                    or variant != 'master'
                    or task in tasks or previous is not None and task.int <= previous.int):
                raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
            tasks.add(task)
            previous = task
        accepted = rejected = completed = 0
        runs = set()
        for run, variant, task in rows:
            try:
                if c.adopt(tenant_id, run): accepted += 1
                else: rejected += 1
                runs.add(run)
            except InterruptedError:
                raise
            except Exception as exc:
                if on_retry is None: raise
                on_retry(exc)
        for run in sorted(runs, key=lambda value: value.int):
            try:
                row = c._query(c.write, tenant_id,
                    'SELECT photo.finalize_successful_v29_perspective_quality(%s,%s)',
                    (tenant_id, run), one=True)
                if not isinstance(row, tuple) or len(row) != 1 or type(row[0]) is not bool:
                    raise AdoptionInvalid('PERSPECTIVE_QUALITY_FINALIZER_INVALID')
                completed += int(row[0])
            except InterruptedError:
                raise
            except Exception as exc:
                if on_retry is None: raise
                on_retry(exc)
        return (previous if len(rows) == 32 else None, accepted, rejected, completed)

    def process_completion_page(self, tenant_id, after=None, *, on_retry=None):
        """Recover committed receipts even when no decode candidates remain."""
        if not isinstance(tenant_id, UUID) or tenant_id.int == 0:
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_TENANT_INVALID')
        if after is not None and (not isinstance(after, UUID) or after.int == 0):
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_CURSOR_INVALID')
        c = self.coordinator
        rows = c._query(c.read, tenant_id,
            'SELECT * FROM photo.pending_v29_perspective_quality_completion(%s,%s)',
            (tenant_id, after))
        if len(rows) > 32:
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
        previous = after
        for item in rows:
            if not isinstance(item, (tuple, list)) or len(item) != 1:
                raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
            run = item[0]
            if (not isinstance(run, UUID) or run.int == 0
                    or previous is not None and run.int <= previous.int):
                raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
            previous = run
        completed = 0
        for (run,) in rows:
            try:
                row = c._query(c.write, tenant_id,
                    'SELECT photo.finalize_successful_v29_perspective_quality(%s,%s)',
                    (tenant_id, run), one=True)
                if not isinstance(row, tuple) or len(row) != 1 or type(row[0]) is not bool:
                    raise AdoptionInvalid('PERSPECTIVE_QUALITY_FINALIZER_INVALID')
                completed += int(row[0])
            except InterruptedError:
                raise
            except Exception as exc:
                if on_retry is None: raise
                on_retry(exc)
        return (previous if len(rows) == 32 else None, completed)

    def process_lifecycle_page(self, tenant_id, after=None, *, on_retry=None):
        """Reconcile source/quota loss, terminal failure and settled quota release.

        Cancellation is a request. Generic cancellation/GC settle writers; this
        function never cleans objects or treats a cancellation request as final.
        """
        if not isinstance(tenant_id, UUID) or tenant_id.int == 0:
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_TENANT_INVALID')
        if after is not None and (not isinstance(after, UUID) or after.int == 0):
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_CURSOR_INVALID')
        c = self.coordinator
        rows = c._query(c.read, tenant_id,
            'SELECT * FROM photo.pending_v29_perspective_quality_lifecycle(%s,%s)',
            (tenant_id, after))
        if len(rows) > 32:
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
        previous = after
        for item in rows:
            if not isinstance(item, (tuple, list)) or len(item) != 1:
                raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
            run = item[0]
            if (not isinstance(run, UUID) or run.int == 0
                    or previous is not None and run.int <= previous.int):
                raise AdoptionInvalid('PERSPECTIVE_QUALITY_PENDING_INVALID')
            previous = run
        counts = {'cancel_requested': 0, 'failed': 0, 'released': 0}
        for (run,) in rows:
            try:
                row = c._query(c.write, tenant_id,
                    'SELECT photo.maintain_v29_perspective_quality(%s,%s)',
                    (tenant_id, run), one=True)
                if (not isinstance(row, tuple) or len(row) != 1
                        or row[0] not in ('none', 'cancel_requested', 'failed', 'released')):
                    raise AdoptionInvalid('PERSPECTIVE_QUALITY_LIFECYCLE_INVALID')
                if row[0] != 'none':
                    counts[row[0]] += 1
            except InterruptedError:
                raise
            except Exception as exc:
                if on_retry is None: raise
                on_retry(exc)
        return (previous if len(rows) == 32 else None, counts)
