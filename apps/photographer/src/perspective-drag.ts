import type {PerspectivePoint} from './perspective-intent';
export function perspectiveImageRect(width:number,height:number,aspect:number){
 if(![width,height,aspect].every(v=>Number.isFinite(v)&&v>0))throw new Error('PERSPECTIVE_VIEWPORT_INVALID');
 const w=Math.min(width,height*aspect),h=w/aspect;
 return {x:(width-w)/2,y:(height-h)/2,width:w,height:h};
}
/** Screen-space gesture deltas map to normalized source pixel-edge coordinates.
 * Refuse crossings instead of silently changing corner order. */
export function dragPerspectiveCorner(corners:PerspectivePoint[],index:number,start:PerspectivePoint,dx:number,dy:number,imageWidth:number,imageHeight:number):PerspectivePoint[]{
 if(corners.length!==4||!Number.isInteger(index)||index<0||index>3||![start.x,start.y,dx,dy,imageWidth,imageHeight].every(Number.isFinite)||imageWidth<=0||imageHeight<=0)throw new Error('PERSPECTIVE_DRAG_INVALID');
 const next=corners.map(point=>({...point}));
 next[index]={x:Math.max(0,Math.min(1,start.x+dx/imageWidth)),y:Math.max(0,Math.min(1,start.y+dy/imageHeight))};
 for(let i=0;i<4;i++){
  const a=next[i],b=next[(i+1)%4],c=next[(i+2)%4];
  if(![a.x,a.y].every(v=>Number.isFinite(v)&&v>=0&&v<=1)||(b.x-a.x)*(c.y-b.y)-(b.y-a.y)*(c.x-b.x)<=0)throw new Error('PERSPECTIVE_DRAG_INVALID');
 }
 return next;
}
