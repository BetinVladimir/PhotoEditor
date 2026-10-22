"""Bounded retention planning over a trusted published-backup catalogue.

No deletion is performed. Publication receipts and complete dependency lists
must be independently verified by the backup authority before calling this.
A retained recovery point retains every prerequisite, including WAL segments.
"""
from dataclasses import dataclass
from datetime import datetime, timezone
from uuid import UUID


@dataclass(frozen=True)
class BackupRetentionPolicy:
    daily: int = 7
    weekly: int = 4
    maximum_records: int = 100000


@dataclass(frozen=True)
class PublishedBackup:
    backup_id: UUID
    recovered_at: datetime
    manifest_sha256: str
    dependencies: tuple[UUID, ...] = ()
    protected: bool = False


@dataclass(frozen=True)
class BackupRetentionPlan:
    retained: tuple[UUID, ...]
    removable: tuple[UUID, ...]


def plan_backup_retention(records, policy, *, now, check_active):
    if (not isinstance(policy, BackupRetentionPolicy) or
            any(type(v) is not int for v in (policy.daily, policy.weekly, policy.maximum_records)) or
            not 1 <= policy.daily <= 366 or not 1 <= policy.weekly <= 520 or
            not 1 <= policy.maximum_records <= 1000000 or not callable(check_active) or
            not isinstance(now, datetime) or now.utcoffset() is None):
        raise ValueError('BACKUP_RETENTION_POLICY')
    def active():
        if check_active() is False:
            raise RuntimeError('BACKUP_RETENTION_INACTIVE')
    now = now.astimezone(timezone.utc)
    catalogue = {}
    edges = 0
    for record in records:
        active()
        if len(catalogue) >= policy.maximum_records:
            raise ValueError('BACKUP_RETENTION_LIMIT')
        if (not isinstance(record, PublishedBackup) or
                not isinstance(record.backup_id, UUID) or record.backup_id.int == 0 or
                record.backup_id in catalogue or not isinstance(record.recovered_at, datetime) or
                record.recovered_at.utcoffset() is None or record.recovered_at > now or
                type(record.protected) is not bool or not isinstance(record.dependencies, tuple) or
                len(record.dependencies) > policy.maximum_records or
                any(not isinstance(v, UUID) or v.int == 0 or v == record.backup_id for v in record.dependencies) or
                len(set(record.dependencies)) != len(record.dependencies) or
                not isinstance(record.manifest_sha256, str) or len(record.manifest_sha256) != 64 or
                any(v not in '0123456789abcdef' for v in record.manifest_sha256)):
            raise ValueError('BACKUP_RETENTION_CATALOGUE')
        edges += len(record.dependencies)
        if edges > policy.maximum_records * 8:
            raise ValueError('BACKUP_RETENTION_LIMIT')
        catalogue[record.backup_id] = record
    # Validate all chains, including expired points, before authorizing any prune.
    # Iterative DFS avoids recursion limits and rejects missing/cyclic prerequisites.
    done = set()
    for identity in catalogue:
        stack = [(identity, False)]
        visiting = set()
        while stack:
            active()
            current, exiting = stack.pop()
            if exiting:
                visiting.remove(current); done.add(current); continue
            if current in visiting:
                raise ValueError('BACKUP_RETENTION_CYCLE')
            if current in done:
                continue
            if current not in catalogue:
                raise ValueError('BACKUP_RETENTION_DEPENDENCY')
            visiting.add(current); stack.append((current, True))
            for dependency in catalogue[current].dependencies:
                stack.append((dependency, False))
    ordered = sorted(catalogue.values(), key=lambda v: (v.recovered_at, v.backup_id.int), reverse=True)
    retained, days, weeks = set(), set(), set()
    for record in ordered:
        active()
        stamp = record.recovered_at.astimezone(timezone.utc)
        day, week = stamp.date(), stamp.isocalendar()[:2]
        if record.protected or (day not in days and len(days) < policy.daily) or (week not in weeks and len(weeks) < policy.weekly):
            retained.add(record.backup_id)
        if day not in days and len(days) < policy.daily: days.add(day)
        if week not in weeks and len(weeks) < policy.weekly: weeks.add(week)
    pending = list(retained)
    while pending:
        active()
        for dependency in catalogue[pending.pop()].dependencies:
            if dependency not in retained:
                retained.add(dependency); pending.append(dependency)
    active()
    key = lambda identity: identity.int
    return BackupRetentionPlan(tuple(sorted(retained, key=key)), tuple(sorted(set(catalogue)-retained, key=key)))
