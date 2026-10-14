"""Subprocess-only fault boundary: durable commit before Transport ACK."""
import os
from threading import Event
from uuid import UUID
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.executor import Executor,Operation,OperationResult
from photo_worker.runtime.transport import Transport


def main():
    mode=os.environ['PHOTO_TEST_RESTART_MODE']
    assert mode in ('crash-after-commit','crash-after-claim','restart')
    crash=mode=='crash-after-commit'
    def operation(ctx):
        if mode=='crash-after-claim':
            ctx.check_active()
            print('CLAIM_DURABLE_BEFORE_OPERATION_COMMIT',flush=True)
            os._exit(74)
        if not crash:
            raise AssertionError('Terminal redelivery must not execute the operation')
        ctx.check_active()
        return OperationResult([])
    class BoundaryExecutor(Executor):
        def process(self,raw):
            outcome=super().process(raw)
            if crash:
                assert outcome=='committed'
                print('DURABLE_COMMIT_BEFORE_ACK',flush=True)
                os._exit(73)
            assert outcome=='terminal'
            print('RESTART_TERMINAL_REDELIVERY',flush=True)
            return outcome
    executor=BoundaryExecutor(TaskDatabase(os.environ['PHOTO_TEST_RESTART_DSN']),UUID(os.environ['PHOTO_TEST_RESTART_WORKER']),[Operation('asset.verify','media',operation)])
    assert Transport(os.environ['PHOTO_TEST_RESTART_BROKER'],'media',executor,development=True).consume(Event(),max_messages=1)==1


if __name__=='__main__':
    main()
