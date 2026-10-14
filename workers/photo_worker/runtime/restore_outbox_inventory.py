"""Canonical bounded inventory of pending task envelopes; no publication."""
from hashlib import sha256
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes
from .restore_authority import require_restore_active

DOMAIN = b'photoeditor.restore-task-outbox/v1\n'


def pending_task_inventory(rows, *, expected_count, check_active):
    if type(expected_count) is not int or not 0 <= expected_count <= 10_000_000:
        raise ValueError('RESTORE_OUTBOX_INVENTORY_LIMIT')
    require_restore_active(check_active)
    digest = sha256(DOMAIN)
    previous = None
    count = 0
    for row in rows:
        require_restore_active(check_active)
        if not isinstance(row, (tuple, list)) or len(row) != 6:
            raise ValueError('RESTORE_OUTBOX_INVENTORY_ROW')
        row_id, tenant, event, destination, routing, payload = row
        ids = []
        for value in (row_id, tenant, event):
            if not isinstance(value, (str, UUID)):
                raise ValueError('RESTORE_OUTBOX_INVENTORY_UUID')
            parsed = UUID(str(value))
            if parsed.int == 0 or str(parsed) != str(value):
                raise ValueError('RESTORE_OUTBOX_INVENTORY_UUID')
            ids.append(str(parsed))
        if previous is not None and ids[0] <= previous:
            raise ValueError('RESTORE_OUTBOX_INVENTORY_ORDER')
        if (not isinstance(destination, str) or not destination or len(destination.encode()) > 4096
                or not isinstance(routing, str) or len(routing.encode()) > 4096
                or not isinstance(payload, dict)):
            raise ValueError('RESTORE_OUTBOX_INVENTORY_ENVELOPE')
        raw = canonical_bytes(dict(zip(('id', 'tenant_id', 'event_id', 'destination', 'routing_key', 'payload'),
                                       (*ids, destination, routing, payload))))
        if len(raw) > 1 << 20:
            raise ValueError('RESTORE_OUTBOX_INVENTORY_BYTES')
        count += 1
        if count > expected_count:
            raise ValueError('RESTORE_OUTBOX_INVENTORY_COUNT')
        digest.update(raw + b'\n')
        previous = ids[0]
    require_restore_active(check_active)
    if count != expected_count:
        raise ValueError('RESTORE_OUTBOX_INVENTORY_COUNT')
    return digest.hexdigest()
