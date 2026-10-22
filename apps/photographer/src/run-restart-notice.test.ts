import {it,expect} from 'vitest';
import {runRestartNotice} from './run-restart-notice';
it('explains both compatibility refusals without exposing internal codes',()=>{
 for(const code of ['APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING','APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT']){
  const notice=runRestartNotice([code]);expect(notice).toContain('original collection');expect(notice).not.toContain(code);
 }
});
it('does not infer a restart from unknown or absent diagnostics',()=>{
 expect(runRestartNotice(undefined)).toBeUndefined();expect(runRestartNotice([])).toBeUndefined();
 expect(runRestartNotice(['APPLY_PLAN_RESTART_REQUIRED:UNKNOWN','database connection failed'])).toBeUndefined();
});
