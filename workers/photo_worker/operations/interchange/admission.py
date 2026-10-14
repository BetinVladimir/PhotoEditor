"""SQL-only current fence/project/bundle/reservation authority for parse."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.interchange_archive import ArchiveLimits


class SQLParseAdmission:
    def __init__(self, database):
        if not callable(getattr(database, "interchange_parse_context", None)):
            raise ValueError("INTERCHANGE_ADMISSION_UNAVAILABLE")
        self.database = database

    def admit(self, ctx, params):
        ctx.check_active()
        result = self.database.interchange_parse_context(ctx.lease, canonical_bytes(params))
        ctx.check_active()
        if not isinstance(result, dict) or set(result) != {"archive_bytes", "expanded_bytes", "files"}:
            raise OperationFailure("INTERCHANGE_SOURCE_STALE")
        try:
            limits = ArchiveLimits(result["archive_bytes"], result["expanded_bytes"], result["files"])
            limits.validate()
        except ValueError as exc:
            raise OperationFailure("INTERCHANGE_SOURCE_STALE") from exc
        return limits
