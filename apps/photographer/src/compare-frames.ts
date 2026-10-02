export type CompareFrame={versionId:string;sourceSha:string;previewURI:string;iccSha:string};
export type CompareViewport={scale:number;centerX:number;centerY:number};
export type FrameComparison={frames:CompareFrame[];selected:number;viewport:CompareViewport;decisions:Record<string,'accept'|'reject'>};
/** Caller supplies authorized color-managed previews, never master download URLs. */
export function compareFrames(frames:readonly CompareFrame[],viewport:CompareViewport={scale:1,centerX:.5,centerY:.5}):FrameComparison {
 if(frames.length<2||frames.length>6||new Set(frames.map(frame=>frame.versionId)).size!==frames.length||frames.some(frame=>!frame.versionId||!frame.previewURI||!/^[0-9a-f]{64}$/.test(frame.sourceSha)||!/^[0-9a-f]{64}$/.test(frame.iccSha)||frame.iccSha!==frames[0].iccSha))throw new Error('COMPARE_FRAMES_INVALID');
 const state:FrameComparison={frames:frames.map(frame=>({...frame})),selected:0,viewport:{scale:1,centerX:.5,centerY:.5},decisions:{}};
 return setCompareViewport(state,viewport);
}
export function setCompareViewport(state:FrameComparison,viewport:CompareViewport):FrameComparison {
 if(!Object.values(viewport).every(Number.isFinite)||viewport.scale<1||viewport.scale>16||viewport.centerX<0||viewport.centerX>1||viewport.centerY<0||viewport.centerY>1)throw new Error('COMPARE_VIEWPORT_INVALID');
 return {...state,viewport:{...viewport}};
}
export function selectCompareFrame(state:FrameComparison,step:-1|1):FrameComparison {
 if(step!==-1&&step!==1)throw new Error('COMPARE_SELECTION_INVALID');
 return {...state,selected:(state.selected+step+state.frames.length)%state.frames.length};
}
export function decideCompareFrame(state:FrameComparison,decision:'accept'|'reject'):FrameComparison {
 if(!['accept','reject'].includes(decision))throw new Error('COMPARE_DECISION_INVALID');
 return {...state,decisions:{...state.decisions,[state.frames[state.selected].versionId]:decision}};
}
/** Shared normalized center maps independently to each preview's display box. */
export function compareFrameTransform(viewport:CompareViewport,width:number,height:number):{scale:number;translateX:number;translateY:number} {
 if(!Number.isFinite(width)||!Number.isFinite(height)||width<=0||height<=0)throw new Error('COMPARE_VIEWPORT_INVALID');
 // Reuse validation without trusting a caller-mutated viewport.
 setCompareViewport({frames:[],selected:0,viewport,decisions:{}},viewport);
 return {scale:viewport.scale,translateX:(.5-viewport.centerX)*width*viewport.scale,translateY:(.5-viewport.centerY)*height*viewport.scale};
}
