"""Fail-closed operator authority boundaries shared by restore entrypoints."""


def require_restore_active(check_active):
    if check_active() is False:
        raise RuntimeError('RESTORE_INACTIVE')


def require_restore_authority(check_active, assert_isolated_and_paused):
    require_restore_active(check_active)
    if assert_isolated_and_paused() is False:
        raise RuntimeError('RESTORE_WRITERS_NOT_PAUSED')
    require_restore_active(check_active)


def require_restore_fixture(check_active, assert_fixture_scope, *scope):
    require_restore_active(check_active)
    if assert_fixture_scope(*scope) is False:
        raise RuntimeError('RESTORE_FIXTURE_DENIED')
    require_restore_active(check_active)
