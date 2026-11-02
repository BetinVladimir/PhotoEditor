import {expect,it} from 'vitest';
import {loadVector} from './video-test-vectors';
import {clipDuration,frameTicks,framesToTicks,TICKS_PER_SECOND,ticksToFrame} from './video-timeline-time';
type V={frame_ticks:Record<string,number>;sample_ticks:Record<string,number>;max_ticks_3h:number;max_safe:number;
 frames_to_ticks:{fps:string;frame:number;ticks:number}[];ticks_to_frame:{fps:string;ticks:number;floor:number;round:number}[]};
const v=loadVector<V>('time');
const fps=(s:string)=>{const[n,d]=s.split('/').map(Number);return{num:n,den:d};};
it('frameTicks matches vector and rejects unsupported rates',()=>{
 for(const[k,t]of Object.entries(v.frame_ticks))expect(frameTicks(fps(k))).toBe(t);
 expect(()=>frameTicks({num:11,den:1})).toThrow('FPS_UNSUPPORTED');// card TK-301 names 7/1, but 705600000 % 7 == 0
 expect(()=>frameTicks({num:0,den:1})).toThrow('FPS_UNSUPPORTED');
});
it('sample ticks and limits',()=>{
 for(const[k,t]of Object.entries(v.sample_ticks))expect(TICKS_PER_SECOND/Number(k)).toBe(t);
 expect(3*3600*TICKS_PER_SECOND).toBe(v.max_ticks_3h);expect(Number.MAX_SAFE_INTEGER).toBe(v.max_safe);
});
it('framesToTicks',()=>{
 for(const c of v.frames_to_ticks)expect(framesToTicks(fps(c.fps),c.frame)).toBe(c.ticks);
 expect(()=>framesToTicks(fps('25/1'),-1)).toThrow('FRAME_NEGATIVE');
});
it('ticksToFrame floor/round',()=>{
 for(const c of v.ticks_to_frame){expect(ticksToFrame(fps(c.fps),c.ticks,'floor')).toBe(c.floor);expect(ticksToFrame(fps(c.fps),c.ticks,'round')).toBe(c.round);}
 expect(()=>ticksToFrame(fps('25/1'),-1,'floor')).toThrow('TICKS_NEGATIVE');
});
it('clipDuration for photo, default speed and rational speed',()=>{
 expect(clipDuration({source:{kind:'photo'},duration:99})).toBe(99);
 expect(clipDuration({source:{kind:'video'},src_in:100,src_out:700})).toBe(600);
 expect(clipDuration({source:{kind:'video'},src_in:0,src_out:600,speed:{num:2,den:1}})).toBe(300);
 expect(clipDuration({source:{kind:'video'},src_in:0,src_out:600,speed:{num:1,den:4}})).toBe(2400);
});
