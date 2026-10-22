import {afterEach,expect,it,vi} from 'vitest';
import {waitEditorStorage} from './editor-local-wait';
afterEach(()=>vi.useRealTimers());
it('bounds hung storage without claiming rollback',async()=>{
 vi.useFakeTimers();let resolve!:(value:boolean)=>void;
 const storage=new Promise<boolean>(done=>{resolve=done});
 const wait=waitEditorStorage(storage,new AbortController().signal);
 const rejected=expect(wait).rejects.toThrow('EDITOR_STORAGE_TIMEOUT');
 await vi.advanceTimersByTimeAsync(8000);await rejected;
 resolve(true);await expect(storage).resolves.toBe(true);
});
it('cancels waiting while leaving the original exact storage action to settle',async()=>{
 let finish!:(value:string)=>void;const storage=new Promise<string>(resolve=>{finish=resolve});
 const old=new AbortController(),pending=waitEditorStorage(storage,old.signal);
 const rejected=expect(pending).rejects.toThrow();old.abort();await rejected;
 const replacement=waitEditorStorage(Promise.resolve('new'),new AbortController().signal);
 finish('old');await expect(replacement).resolves.toBe('new');await expect(storage).resolves.toBe('old');
});
it('propagates storage refusal and removes its deadline',async()=>{
 vi.useFakeTimers();await expect(waitEditorStorage(Promise.reject(new Error('CAS_CHANGED')),new AbortController().signal)).rejects.toThrow('CAS_CHANGED');
 expect(vi.getTimerCount()).toBe(0);
});
