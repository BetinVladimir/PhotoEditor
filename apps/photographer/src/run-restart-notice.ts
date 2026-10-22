// Only server-defined compatibility diagnostics select this notice. Unknown
// diagnostics must not become raw technical text in the photographer interface.
const restartCodes = new Set([
 'APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING',
 'APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT',
]);
export function runRestartNotice(errors: readonly string[] | undefined): string | undefined {
 if (!errors?.some(code => restartCodes.has(code))) return undefined;
 return 'This processing run uses an older plan and is being stopped. Once it has stopped, start a new processing run from the original collection. Your original photos are preserved.';
}
