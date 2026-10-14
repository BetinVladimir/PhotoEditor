# Interchange export API installation

API startup accepts `V30_INTERCHANGE_EXPORT_DEPLOYMENT_FILE` (normalized absolute path) and `V30_INTERCHANGE_EXPORT_DEPLOYMENT_SHA` (lowercase SHA256). Both are required together. Existing `interchange-export-deployment/v1` carries pinned native paths, runtime/report hashes and limits; its decoder remains authoritative.

Trusted `apiProcessDependencies.InterchangeExportAuthority` supplies actual schema/model/source/consent authority. It must implement `ExportNativeAuthorityFactory`; it is never loaded from HTTP or an environment-selected module. A competing `InterchangeExport` factory refuses. Without a pinned document an authority declaration refuses; with neither, import-only startup remains available.

`prepareAPIInterchangeExport` decodes and owns deployment bytes before API resource construction. Its returned factory receives the actual API and validated scheduler pools, object store and bucket. `NativeExportDeployment.BuildWithAuthority` installs concrete Python image/mask/array probes, shared scratch and measured resource qualifier, then `NewSQLNativeExportCreationPreparation` builds proposals/execution using the same image authority.

`V30_INTERCHANGE_SCHEDULER_DATABASE_URL` remains mandatory for enabled export. Startup validates SQL contracts before factory invocation. Partial errors/cancellation close installed factory resources before scheduler. Successful shutdown closes export resources once before scheduler shutdown. Native deployment probes own no persistent subprocess; scratch cleanup remains request scoped.

Configuration hashes are not qualification proof. Installed authority must enforce real runtime/model/source/consent contracts; resource measurements match exact workloads. Concrete operator provisioning and full module acceptance remain open. Unit scenarios are authored, NOT RUN; migrations/builds/validators have not run.

Deployment loading requires a normalized absolute path and single-link regular file. Opens use O_NOFOLLOW and O_NONBLOCK, then verify regular-file permissions, size and inode across initial/open/final/current observations. Hardlinks and nonregular files refuse, including FIFO without blocking. Seven additional envelope scenarios authored NOT RUN.
