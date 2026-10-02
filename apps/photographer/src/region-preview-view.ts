import type {EditorState} from './editor-store';
import type {RegionPreviewRequest} from './region-preview';
import type {RegionPreviewScope} from './region-preview-session';

export type RegionGeometry={left:number;top:number;width:number;height:number;output_width:number;output_height:number};
/** Include unsaved edits and undo/redo generations, not just the persisted CAS revision. */
export function editorRegionScope(scope:Omit<RegionPreviewScope,'revision'>,state:EditorState):RegionPreviewScope {
 return {...scope,revision:JSON.stringify([state.versionId,state.sourceSha,state.draftRevision,state.requestRevision])};
}
/** Canvas dimensions are those after complete recipe geometry, including orientation. */
export function regionGeometry(width:number,height:number,rect:RegionPreviewRequest['rect'],maxEdge:number):RegionGeometry {
 if(!Number.isSafeInteger(width)||!Number.isSafeInteger(height)||width<1||height<1||width>200000000||height>Math.floor(200000000/width)||!Number.isInteger(maxEdge)||maxEdge<64||maxEdge>1024)throw new Error('REGION_GEOMETRY_INVALID');
 const {x0,y0,x1,y1}=rect;
 if([x0,y0,x1,y1].some(n=>typeof n!=='number'||!Number.isFinite(n)||n<0||n>1)||x1<=x0||y1<=y0)throw new Error('REGION_GEOMETRY_INVALID');
 const left=Math.floor(x0*width),top=Math.floor(y0*height),w=Math.ceil(x1*width)-left,h=Math.ceil(y1*height)-top;
 const scale=Math.min(1,maxEdge/Math.max(w,h));
 return {left,top,width:w,height:h,output_width:Math.max(1,Math.floor(w*scale)),output_height:Math.max(1,Math.floor(h*scale))};
}
/** Display density is explicit; reduced derivatives must never be labelled source 100%. */
export function regionRenderedScale(geometry:RegionGeometry):{x:number;y:number;sourcePixels:boolean} {
 const values=[geometry.left,geometry.top,geometry.width,geometry.height,geometry.output_width,geometry.output_height];
 if(values.some(n=>!Number.isSafeInteger(n))||geometry.left<0||geometry.top<0||geometry.width<1||geometry.height<1||geometry.output_width<1||geometry.output_height<1||geometry.output_width>geometry.width||geometry.output_height>geometry.height||geometry.output_width>1024||geometry.output_height>1024)throw new Error('REGION_GEOMETRY_INVALID');
 return {x:geometry.output_width/geometry.width,y:geometry.output_height/geometry.height,sourcePixels:geometry.output_width===geometry.width&&geometry.output_height===geometry.height};
}
