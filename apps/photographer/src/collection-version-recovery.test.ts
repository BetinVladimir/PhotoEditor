import {expect,it,vi} from 'vitest';
import {resumeCollectionVersion} from './collection-version-recovery';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const selection={collection:id(1),item:id(2),asset:id(3),revision:'7',expectedVersion:id(4),version:id(5)};
it('recovers an already applied selection without another mutation',async()=>{
 const api={reconcileCollectionVersion:vi.fn().mockResolvedValue('applied'),replaceCollectionVersion:vi.fn()};
 await resumeCollectionVersion(api,selection);expect(api.replaceCollectionVersion).not.toHaveBeenCalled();
});
it('reconciles a lost CAS response against the exact saved selection',async()=>{
 const api={reconcileCollectionVersion:vi.fn().mockResolvedValueOnce('unchanged').mockResolvedValueOnce('applied'),replaceCollectionVersion:vi.fn().mockRejectedValue(new Error('lost'))};
 await resumeCollectionVersion(api,selection);expect(api.replaceCollectionVersion).toHaveBeenCalledTimes(1);
 expect(api.reconcileCollectionVersion.mock.calls.every(args=>JSON.stringify(args[0])===JSON.stringify(selection))).toBe(true);
});
it('refuses concurrent edits without refreshing the frozen revision',async()=>{
 const api={reconcileCollectionVersion:vi.fn().mockResolvedValue('conflict'),replaceCollectionVersion:vi.fn()};
 await expect(resumeCollectionVersion(api,selection)).rejects.toThrow('CONFLICT');expect(api.replaceCollectionVersion).not.toHaveBeenCalled();
});
it('does not mutate after cancellation during the first read',async()=>{
 const controller=new AbortController(),api={reconcileCollectionVersion:vi.fn(async()=>{controller.abort();return 'unchanged' as const}),replaceCollectionVersion:vi.fn()};
 await expect(resumeCollectionVersion(api,selection,controller.signal)).rejects.toThrow();expect(api.replaceCollectionVersion).not.toHaveBeenCalled();
});
