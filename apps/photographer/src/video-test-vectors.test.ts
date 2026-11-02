import {expect,it} from 'vitest';
import {loadVector} from './video-test-vectors';
it('loads time vector with exact 25 fps tick count',()=>{
 const v=loadVector<{frame_ticks:Record<string,number>}>('time');expect(v.frame_ticks['25/1']).toBe(28224000);
});
it('rejects path-like vector names',()=>{expect(()=>loadVector('../x')).toThrow();});
