import type {Command,EditResult,TimelineDocument} from '@photoeditor/api-client';
import {deleteClip,moveClip,setSpeed,splitClip,trimEdge} from './video-timeline-commands';
import {applyCuts,type ApplyCutsMode,type CutBoundary} from './video-timeline-cuts';

const clone=<T,>(v:T):T=>JSON.parse(JSON.stringify(v));

/** FN-T-14: every command is invertible by restoring the document it was applied to. */
export function makeCommand(name:string,run:(doc:TimelineDocument)=>EditResult):Command{
 const cmd:Command={
  name,
  apply:run,
  invert:(before)=>{
   const snapshot=clone(before);
   const restore:Command={name:`undo:${name}`,apply:()=>({ok:true,doc:clone(snapshot)}),invert:()=>cmd};
   return restore;
  },
 };
 return cmd;
}
export const invertCommand=(before:TimelineDocument,command:Command):Command=>command.invert(before);

export const splitCommand=(clip:string,at:number,newId:string)=>makeCommand('split_clip',d=>splitClip(d,clip,at,newId));
export const trimCommand=(clip:string,edge:'in'|'out',pos:number,sourceDuration:number)=>makeCommand('trim_edge',d=>trimEdge(d,clip,edge,pos,sourceDuration));
export const moveCommand=(clip:string,newAt:number,track?:string)=>makeCommand('move_clip',d=>moveClip(d,clip,newAt,track));
export const deleteCommand=(clip:string,ripple:boolean)=>makeCommand('delete_clip',d=>deleteClip(d,clip,ripple));
export const speedCommand=(clip:string,num:number,den:number)=>makeCommand('set_speed',d=>setSpeed(d,clip,num,den));
export const applyCutsCommand=(mediaId:string,b:CutBoundary[],accepted:string[],mode:ApplyCutsMode,newIds:string[])=>makeCommand('apply_cuts',d=>applyCuts(d,mediaId,b,accepted,mode,newIds));
