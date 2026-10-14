"""Bounded single-process queue runner with durable retry scheduling."""

import random
import time

from .camera import WatchFolderAdapter
from .http import APIError
from .transfer import SourceChanged, Transfer


class Runner:
    def __init__(self, transfer: Transfer, clock=time.time):
        self.transfer, self.clock = transfer, clock
        self.camera = WatchFolderAdapter(transfer.watch)
        self.camera.connect(transfer.watch.device_id)

    def tick(self) -> int:
        watch, journal = self.transfer.watch, self.transfer.journal
        self.camera.capture_events(self.clock())
        processed = 0
        for entry in journal.due(watch.device_id, self.clock(), limit=2):
            try:
                result = self.transfer.process(entry.device_id, entry.sequence)
                if result.state != "done":
                    journal.delay(entry.device_id, entry.sequence, self.clock() + 5)
            except APIError as error:
                if error.status in (401, 403):
                    raise RuntimeError("device authorization unavailable; operator action required") from None
                if error.status == 409 and error.code == "CAPTURE_BEGIN_RECONCILIATION_REQUIRED":
                    journal.hold_for_reconciliation(entry.device_id, entry.sequence,
                                                    self.clock() + 300)
                    processed += 1
                    continue
                if error.status == 503 and error.code == "QUALIFICATION_UNAVAILABLE":
                    journal.hold_for_qualification(entry.device_id, entry.sequence,
                                                   self.clock() + 300)
                    processed += 1
                    continue
                if error.status in (408, 425, 429) or error.status >= 500:
                    self._retry(entry, "api_retry")
                else:
                    self._fail(entry, "api_rejected")
            except (ConnectionError, TimeoutError, OSError):
                self._retry(entry, "transport_retry")
            except SourceChanged:
                self._fail(entry, "source_changed")
            except (ValueError, KeyError, TypeError):
                self._fail(entry, "protocol_invalid")
            processed += 1
        return processed

    def _retry(self, entry, reason):
        current = self.transfer.journal.get(entry.device_id, entry.sequence)
        delay = min(300, 2 ** min(current.retries + 1, 9)) + random.uniform(0, 1)
        self.transfer.journal.delay(entry.device_id, entry.sequence, self.clock() + delay, reason)

    def _fail(self, entry, reason):
        current = self.transfer.journal.get(entry.device_id, entry.sequence)
        self.transfer.journal.fail(entry.device_id, entry.sequence, current.state, reason)
