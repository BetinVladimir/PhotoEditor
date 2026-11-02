/** FN-T-13: nearest anchor within threshold (tie → earlier), otherwise the candidate rounded to the frame grid. */
export function snapTime(candidate:number,anchors:number[],thresholdTicks:number,frameTicks:number):number{
 let best:number|null=null,bestD=Infinity;
 for(const a of anchors){
  const d=Math.abs(a-candidate);
  if(d>thresholdTicks)continue;
  if(d<bestD||(d===bestD&&best!==null&&a<best)){best=a;bestD=d;}
 }
 if(best!==null)return best;
 return Math.floor((candidate+Math.floor(frameTicks/2))/frameTicks)*frameTicks;
}
