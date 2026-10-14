"""Dedicated signer process composition, using targeted worker transport.

Trusted provisioning supplies a fully qualified operator/executor factory.
There is intentionally no environment-selected Python module/import hook.
"""
from threading import current_thread, main_thread

from .artifacts import ArtifactConfig
from .gateway import client_tls
from .database import TaskDatabase
from .executor import Executor
from .service import WorkerConfigurationError, run_built_worker, _with_policy_thread_budget
from .transport import Transport


def build_signer_worker(config, executor_factory):
    """Build only provenance.sign; reject unrelated imaging installations.

    executor_factory(database, worker_id, artifacts) must use
    build_native_signer_executor or build_signer_executor, which enforce live
    native authority and exact SQL signer registration before constructing it.
    The shared lifecycle independently rechecks registered operations at start.
    """
    if (config.resource_class != 'cpu' or config.backend != 'cpu'
            or config.delivery_enabled or config.interchange_enabled
            or any(getattr(config, name, None) is not None for name in (
                'icc_root', 'model_root', 'model_lock', 'lens_registry', 'layer_runtime_lock',
                'layer_runtime_sha', 'person_runtime_locks', 'depth_runtime_locks'))
            or not callable(executor_factory)):
        raise WorkerConfigurationError('Signer requires an isolated CPU installation')
    tls = client_tls(config.ca, config.certificate, config.private_key)
    artifacts = ArtifactConfig(config.scratch_root, config.gateway_origin, tls,
        config.max_scratch_bytes, config.max_download_bytes, config.max_upload_bytes)
    database = TaskDatabase(config.database_url)
    def registered_signer_only():
        return database.runtime_operations(config.worker_id, 'cpu') == frozenset({'provenance.sign'})
    if not registered_signer_only():
        raise WorkerConfigurationError('Signer registration must contain only provenance.sign')
    executor = executor_factory(database, config.worker_id, artifacts)
    if not registered_signer_only():
        raise WorkerConfigurationError('Signer registration changed during installation')
    if (not isinstance(executor, Executor) or executor.worker_id != config.worker_id
            or executor.database is not database
            or frozenset(executor.operations) != frozenset({'provenance.sign'})):
        raise WorkerConfigurationError('Signer executable operations are not isolated')
    return database, Transport(config.amqp_url, 'cpu', executor,
        development=config.development, targeted=True)


@_with_policy_thread_budget
def run_signer_worker(config, executor_factory):
    if current_thread() is not main_thread():
        raise WorkerConfigurationError('Signer must run on process main thread')
    database, transport = build_signer_worker(config, executor_factory)
    run_built_worker(config, database, transport)


@_with_policy_thread_budget
def run_deployed_signer(config, deployment_root, *, sdk, pkcs11, library,
                        native_authority, native_inspector, verify_binding,
                        runtime_files, manifest='installation.json',
                        check_active=lambda: None, challenge_listener=None):
    """Load installed configuration and run the isolated worker lifecycle.

    Provisioning supplies already-loaded native handles and independent live
    authorities. No task/env module imports, synthetic qualification or PIN in
    command arguments. File locks are mandatory on this deployment entrypoint;
    they complement rather than replace loaded-handle/native attestation.
    """
    from .signer_deployment import load_signer_deployment
    from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles
    if current_thread() is not main_thread():
        raise WorkerConfigurationError('Signer must run on process main thread')
    if (not isinstance(runtime_files, PinnedRuntimeFiles)
            or not all(callable(value) for value in
                       (native_authority, native_inspector, verify_binding, check_active))):
        raise WorkerConfigurationError('Signer requires pinned native deployment authority')
    from .signer_challenge_lifecycle import SigningChallengeListenerInstallation
    if challenge_listener is not None and not isinstance(challenge_listener, SigningChallengeListenerInstallation):
        raise WorkerConfigurationError('Signer challenge listener installation required')
    def caller_active():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    caller_active()
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    from photo_worker.contracts.provenance import SigningPolicy, decode_provenance
    # File hashing is part of startup admission. Keep its chunk-level activity
    # checks attached to the caller until every installed policy and file agrees.
    with signing_task_scope(caller_active):
        deployment = load_signer_deployment(deployment_root, manifest)
        for raw in deployment.policies:
            caller_active()
            if decode_provenance(raw, SigningPolicy, 1 << 20).sdk_lock_sha != runtime_files.sdk_lock_sha:
                raise WorkerConfigurationError('Signer runtime lock differs from installed policy')
        runtime_files.check()

    listener_guard = None
    live = True
    operators = []
    def active():
        if not live:
            raise RuntimeError('SIGNER_TASK_SCOPE_CLOSED')
        caller_active()
        if listener_guard is not None and listener_guard() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    def authority(*args):
        active()
        result = native_authority(*args)
        active()
        return result
    def capture(operator):
        from photo_worker.operations.provenance.installation import InstalledSigningOperator
        if not isinstance(operator, InstalledSigningOperator):
            raise WorkerConfigurationError('Signer requires installed operator handoff')
        if operators:raise WorkerConfigurationError('Signer operator constructed twice')
        operators.append(operator)
    def executor_factory(database, worker_id, artifacts):
        active()
        options = dict(sdk=sdk, pkcs11=pkcs11, library=library,
            native_authority=authority, native_inspector=native_inspector,
            verify_binding=verify_binding, runtime_files=runtime_files, check_active=active)
        if challenge_listener is not None:options['on_operator'] = capture
        return deployment.build_executor(database, worker_id, artifacts, **options)

    try:
        if challenge_listener is None:
            run_signer_worker(config, executor_factory)
            return
        database, transport = build_signer_worker(config, executor_factory)
        if len(operators) != 1:
            raise WorkerConfigurationError('Signer challenge requires shared installed operator')
        with challenge_listener.running(deployment.policies, operators[0], check_active=caller_active) as guard:
            listener_guard = guard
            active()
            run_built_worker(config, database, transport)
    finally:
        # Native code may retain callbacks after either worker lifecycle exits.
        # A stopped challenge listener is not required for this close fence.
        live = False
