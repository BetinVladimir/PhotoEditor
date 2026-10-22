import {expect,it,vi} from 'vitest';
import {perspectiveApplyTransport} from './perspective-apply-transport';
import type {PerspectiveApplyTransport} from './perspective-apply-attempt';
it('rechecks live authority on each mutation while permitting reconciliation',async()=>{
 let active=true;
 const api={applyPerspective:vi.fn(),replaceCollectionVersion:vi.fn(),reconcileCollectionVersion:vi.fn().mockResolvedValue('unchanged')} as unknown as PerspectiveApplyTransport;
 const transport=perspectiveApplyTransport(api,()=>active);
 const selection={} as Parameters<PerspectiveApplyTransport['replaceCollectionVersion']>[0];
 await transport.reconcileCollectionVersion(selection);active=false;
 expect(()=>transport.replaceCollectionVersion(selection)).toThrow('PERSPECTIVE_APPLY_UNAVAILABLE');
 expect(()=>transport.applyPerspective({} as never,true,false)).toThrow('PERSPECTIVE_APPLY_UNAVAILABLE');
 expect(api.replaceCollectionVersion).not.toHaveBeenCalled();expect(api.applyPerspective).not.toHaveBeenCalled();
 active=true;transport.replaceCollectionVersion(selection);expect(api.replaceCollectionVersion).toHaveBeenCalledOnce();
});
it('does not mutate with an aborted request',()=>{
 const api={applyPerspective:vi.fn(),replaceCollectionVersion:vi.fn(),reconcileCollectionVersion:vi.fn()} as unknown as PerspectiveApplyTransport;
 const controller=new AbortController();controller.abort();
 expect(()=>perspectiveApplyTransport(api,()=>true).applyPerspective({} as never,true,false,controller.signal)).toThrow('PERSPECTIVE_APPLY_UNAVAILABLE');
 expect(api.applyPerspective).not.toHaveBeenCalled();
});
