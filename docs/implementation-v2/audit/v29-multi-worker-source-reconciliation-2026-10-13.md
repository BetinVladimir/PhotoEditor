# V29 panorama/focus worker source reconciliation — 2026-10-13

The production worker CLI resolves installations for both `optical.panorama` and `optical.focus_stack` through `multi_installation_from_environment`. `build_worker` requires unique CPU operations, a CPU backend and an ICC registry, then registers both via `multi_operations`. The adopter CLI independently resolves `multi_adopter_from_environment` for both operations. The worker adapters, SQL admission, runtime lock, source closure, evidence and adoption code paths exist in the current tree. The older `partial_runtime_path` labels do not establish missing operation registration.

Actual completion remains unproved: configured runtime/model lock pins, source and color qualification, real image output quality, scheduler/adopter SQL migration state, cancellation and replay, and routed client workflows need current acceptance evidence. No tests, builds, validators or migrations were run during this source reconciliation.
