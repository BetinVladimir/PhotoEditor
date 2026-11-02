import {expect,it} from 'vitest';
import {loadVector} from './video-test-vectors';
import {derivativeEstimate,renderEstimate} from './video-quota-estimate';
const v=loadVector<{derivative_estimate:{bytes:number;estimate:number}[];render_estimate:{duration_ticks:number;maxrate_bps:number;audio_bps:number;estimate:number}[]}>('quota');
it('derivative_estimate vectors',()=>{for(const c of v.derivative_estimate)expect(derivativeEstimate(c.bytes)).toBe(c.estimate);});
it('render_estimate vectors',()=>{for(const c of v.render_estimate)expect(renderEstimate(c.duration_ticks,c.maxrate_bps,c.audio_bps)).toBe(c.estimate);});
it('throws ESTIMATE_OVERFLOW instead of losing precision',()=>{
 expect(()=>renderEstimate(Number.MAX_SAFE_INTEGER,Number.MAX_SAFE_INTEGER,0)).toThrow('ESTIMATE_OVERFLOW');
 expect(()=>derivativeEstimate(-1)).toThrow('ESTIMATE_INVALID');
});
