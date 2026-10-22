import {expect,it,vi} from 'vitest';
import {perspectiveDetectionAPI} from './perspective-detection-api';
import type {OwnerAPI} from './owner-api';
it('rechecks detector availability before mutations and preserves read recovery',async()=>{
 let available=true;
 const api={get:vi.fn(async()=>({state:'completed'})),perspectiveDetectedLines:vi.fn(),submitPerspectiveLineDetection:vi.fn(async()=>({state:'queued'})),createRenderInputSnapshot:vi.fn()} as unknown as OwnerAPI;
 const guarded=perspectiveDetectionAPI(api,()=>available);
 const args=['project',{} as never,'original-key',undefined] as const;
 await guarded.submitPerspectiveLineDetection(...args);
 expect(api.submitPerspectiveLineDetection).toHaveBeenCalledWith(...args);
 available=false;
 expect(()=>guarded.submitPerspectiveLineDetection(...args)).toThrow('OPERATION_UNAVAILABLE');
 expect(()=>guarded.createRenderInputSnapshot('project',{} as never,'new-key')).toThrow('OPERATION_UNAVAILABLE');
 expect(api.submitPerspectiveLineDetection).toHaveBeenCalledTimes(1);expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
 await guarded.get('/status');expect(api.get).toHaveBeenCalledWith('/status');
});
