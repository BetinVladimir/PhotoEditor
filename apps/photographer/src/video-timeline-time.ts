export const TICKS_PER_SECOND=705_600_000;
export type Rational={num:number;den:number};
export type TimedClip={source:{kind:string};duration?:number;src_in?:number;src_out?:number;speed?:Rational};
const isInt=(n:number)=>Number.isSafeInteger(n);
const TPS=BigInt(TICKS_PER_SECOND);
/** FN-T-01: ticks per frame; throws FPS_UNSUPPORTED when the rate does not divide the timebase. */
export function frameTicks(fps:Rational):number{
 if(!isInt(fps.num)||!isInt(fps.den)||fps.num<=0||fps.den<=0)throw new Error('FPS_UNSUPPORTED');
 const v=TPS*BigInt(fps.den),n=BigInt(fps.num);
 if(v%n!==0n)throw new Error('FPS_UNSUPPORTED');
 return Number(v/n);
}
/** FN-T-02 */
export function framesToTicks(fps:Rational,frame:number):number{
 if(!isInt(frame)||frame<0)throw new Error('FRAME_NEGATIVE');
 const t=BigInt(frame)*BigInt(frameTicks(fps));
 if(t>BigInt(Number.MAX_SAFE_INTEGER))throw new Error('TICKS_OVERFLOW');
 return Number(t);
}
/** FN-T-03: integer division, no float rounding. */
export function ticksToFrame(fps:Rational,ticks:number,mode:'floor'|'round'):number{
 if(!isInt(ticks)||ticks<0)throw new Error('TICKS_NEGATIVE');
 const ft=BigInt(frameTicks(fps)),t=BigInt(ticks);
 return Number(mode==='floor'?t/ft:(t+ft/2n)/ft);
}
/** FN-T-04 */
export function clipDuration(clip:TimedClip):number{
 if(clip.source.kind==='photo')return clip.duration as number;
 const s=clip.speed??{num:1,den:1};
 return Number(((BigInt(clip.src_out as number)-BigInt(clip.src_in as number))*BigInt(s.den))/BigInt(s.num));
}
