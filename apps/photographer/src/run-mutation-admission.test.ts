import {expect,it,vi} from 'vitest';
import {admitRunMutation} from './run-mutation-admission';
import {runMutationDefinitelyRejected} from './run-mutation-recovery';
it('does not send after scope replacement while reading the journal',async()=>{
 const send=vi.fn();await expect(admitRunMutation(()=>false,undefined,send)).rejects.toThrow('RUN_MUTATION_SCOPE_CHANGED');expect(send).not.toHaveBeenCalled();
});
it('preserves pending recovery when scope changes during transport',async()=>{
 let current=true;
 try{await admitRunMutation(()=>current,undefined,async()=>{current=false;return {revision:'2'};});throw new Error('expected rejection');}
 catch(error){expect((error as Error).message).toBe('RUN_MUTATION_SCOPE_CHANGED');expect(runMutationDefinitelyRejected('retry',error)).toBe(false);expect(runMutationDefinitelyRejected('cancel',error)).toBe(false);}
});
it('returns accepted mutation only while scope remains current',async()=>{
 const result={revision:'2'};expect(await admitRunMutation(()=>true,new AbortController().signal,async()=>result)).toBe(result);
});
it('does not send an aborted request',async()=>{
 const controller=new AbortController();controller.abort();const send=vi.fn();
 await expect(admitRunMutation(()=>true,controller.signal,send)).rejects.toThrow('RUN_MUTATION_SCOPE_CHANGED');expect(send).not.toHaveBeenCalled();
});
