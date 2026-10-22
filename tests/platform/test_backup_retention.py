from datetime import datetime, timedelta, timezone
from uuid import UUID
import pytest
from photo_worker.runtime.backup_retention import (
    BackupRetentionPolicy, PublishedBackup, plan_backup_retention)

NOW = datetime(2026, 10, 17, 12, tzinfo=timezone.utc)
def item(n, days=0, **kwargs):
    return PublishedBackup(UUID(int=n), NOW-timedelta(days=days), 'a'*64, **kwargs)
def plan(records, **kwargs):
    return plan_backup_retention(records, BackupRetentionPolicy(**kwargs), now=NOW, check_active=lambda:None)


def test_daily_weekly_and_dependency_closure_preserve_old_base_and_wal():
    records=[item(n+1,n) for n in range(42)]
    records[0]=item(1,dependencies=(UUID(int=40),))
    records[39]=item(40,39,dependencies=(UUID(int=42),))
    result=plan(records)
    assert {UUID(int=n) for n in range(1,8)} <= set(result.retained)
    assert {UUID(int=40),UUID(int=42)} <= set(result.retained)
    assert set(result.retained).isdisjoint(result.removable)
    assert set(result.retained)|set(result.removable)=={v.backup_id for v in records}
    assert result==plan(reversed(records))


def test_weekly_selection_uses_iso_year_and_keeps_protected_points():
    records=[item(1),item(2,50,protected=True),item(3,51)]
    result=plan(records,daily=1,weekly=1)
    assert result.retained==(UUID(int=1),UUID(int=2))
    assert result.removable==(UUID(int=3),)


@pytest.mark.parametrize('records',[
    [item(1),item(1)], [item(1,dependencies=(UUID(int=9),))],
    [item(1,dependencies=(UUID(int=2),)),item(2,dependencies=(UUID(int=1),))],
    [PublishedBackup(UUID(int=1),NOW.replace(tzinfo=None),'a'*64)],
    [item(1,-1)],
])
def test_invalid_catalogue_never_produces_prune_authority(records):
    with pytest.raises(ValueError):plan(records)


def test_bound_and_cancellation():
    with pytest.raises(ValueError,match='LIMIT'):plan([item(1),item(2)],maximum_records=1)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):
        plan_backup_retention([item(1)],BackupRetentionPolicy(),now=NOW,check_active=cancel)


def test_iso_week_crosses_calendar_year_without_duplicate_week_slot():
    now=datetime(2027,1,3,12,tzinfo=timezone.utc)
    records=[PublishedBackup(UUID(int=1),now,'a'*64),
             PublishedBackup(UUID(int=2),datetime(2026,12,31,12,tzinfo=timezone.utc),'b'*64),
             PublishedBackup(UUID(int=3),datetime(2026,12,25,12,tzinfo=timezone.utc),'c'*64)]
    result=plan_backup_retention(records,BackupRetentionPolicy(daily=1,weekly=2),now=now,check_active=lambda:None)
    assert result.retained==(UUID(int=1),UUID(int=3))
    assert result.removable==(UUID(int=2),)


def test_revoked_operator_guard_cannot_return_removal_plan():
    with pytest.raises(RuntimeError,match='BACKUP_RETENTION_INACTIVE'):
        plan_backup_retention([item(1),item(2,50)],BackupRetentionPolicy(daily=1,weekly=1),now=NOW,check_active=lambda:False)


@pytest.mark.parametrize('dependencies', [([],), ({},), ('not-a-uuid',), (UUID(int=0),), (UUID(int=2), UUID(int=2))])
def test_malformed_dependencies_are_rejected_before_hashing(dependencies):
    with pytest.raises(ValueError, match='BACKUP_RETENTION_CATALOGUE'):
        plan([item(1, dependencies=dependencies)])


def test_even_an_expired_cycle_prevents_any_removal_plan():
    records=[item(1), item(2, 50, dependencies=(UUID(int=3),)),
             item(3, 51, dependencies=(UUID(int=2),))]
    with pytest.raises(ValueError, match='BACKUP_RETENTION_CYCLE'):
        plan(records, daily=1, weekly=1)


def test_empty_catalogue_still_requires_active_authority():
    with pytest.raises(RuntimeError, match='BACKUP_RETENTION_INACTIVE'):
        plan_backup_retention([], BackupRetentionPolicy(), now=NOW, check_active=lambda:False)
