# Additive processing-worker requalification

Use `deploy/requalify-processing-worker.sql` as a privileged deployment operator, never as an application API or initial activation shortcut. It creates no roles, persistent functions, nodes, slots, models or credentials. Initial provisioning/activation remains separate. This operation cannot add `render.compose` or remove existing operation qualifications.

First pause dispatch publishers/producers and verify the scheduler remains stopped, stop only the intended processing worker, and independently prove its old PID has exited and its cgroup is empty. Keep the media/GPU workers and their identities unchanged. Verify Rabbit's actual configured vhost, no ready/unacknowledged messages and no unexpected consumers. Review the six existing model-free CPU adapters and their real execution evidence; model doctor results are not photographic quality qualification. No new Render qualification is conferred.

Capture exact fresh JSON rows using `to_jsonb` for the intended worker/node and all its slots ordered by `slot_name`. Supply a single psql variable `processing_requalification_json` with:

- `expected_worker`, `expected_node`, `expected_slots`: full row snapshots, including current enabled flags, certificate, principal, model lock, old operation inventory, policy, doctor digest and budgets.
- `resource_class`: cpu, media or archive; backend remains CPU.
- `new_operations`: the complete additive inventory mapping each operation to `["cpu"]`; every old mapping remains identical. Registry v1 class/backend and doctor passed model/backend evidence are checked for every entry.
- `doctor_report_jcs`, `doctor_report_file_sha`: exact previously accepted canonical doctor file and its existing node digest. This operator does not replace or reset the doctor.
- `preflight`: status `OPERATOR_REVIEWED_PROCESSING_ADDITIONS`, fresh ISO `reviewed_at` (within ten minutes), exact `worker_id`, true `worker_stopped`, `scheduler_stopped`, `rabbit_empty`, and SHA256 fields `runtime_lock_sha256`, `host_evidence_sha256`, `operation_evidence_sha256` referring to actual reviewed artifacts.

Execute with ON_ERROR_STOP while holding the shared execution lock. SQL locks the node, worker and slots, checks complete CAS equality, idle slots/tasks/dispatch bindings/reservations, restricted login authority and unchanged policy budget/slot agreement. The transaction disables, changes only the operation inventory, and reenables the same rows atomically. It preserves credentials, policy, capacity budgets, doctor, drain fields/generation and all slots. It invalidates heartbeat/policy freshness and sets reported node availability to zero; only a new authenticated heartbeat can expose capacity.

Restart the same pinned launcher/source and limits. Independently verify source gate, node/worker identity, model/certificate/doctor/policy, kernel limits, empty queue with exactly one intended consumer, and fresh authenticated heartbeat before restoring the exact publisher. Save the old/new operation inventories and all receipts.

On SQL failure the whole transaction rolls back. Leave the stopped worker and publisher stopped until the reason is reviewed; do not manually reset registrations or reuse initial activation. If post-start verification fails, stop the worker and retain evidence. Removing an already adopted qualification is deliberately outside this additive operator: use a separately reviewed revocation procedure, never a silent inverse update. No tasks or snapshots are rewritten.
