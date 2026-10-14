import type {PerspectiveLine} from './perspective-lines';
import type {PerspectivePoint} from './perspective-intent';
export function dragPerspectiveLineEndpoint(lines:PerspectiveLine[],index:number,endpoint:'a'|'b',start:PerspectivePoint,dx:number,dy:number,width:number,height:number):PerspectiveLine[]{
 const invalid=()=>{throw new Error('PERSPECTIVE_LINE_DRAG_INVALID');};
 if(!Array.isArray(lines)||lines.length<4||lines.length>1000||!Number.isInteger(index)||index<0||index>=lines.length||(endpoint!=='a'&&endpoint!=='b')||![start.x,start.y,dx,dy,width,height].every(Number.isFinite)||width<=0||height<=0||start.x<0||start.x>1||start.y<0||start.y>1)invalid();
 const next=lines.map(line=>({...line,a:{...line.a},b:{...line.b}}));
 next[index][endpoint]={x:Math.max(0,Math.min(1,start.x+dx/width)),y:Math.max(0,Math.min(1,start.y+dy/height))};
 for(const line of next){
  if(line.family!=='horizontal'&&line.family!=='vertical')invalid();
  for(const point of [line.a,line.b])if(![point.x,point.y].every(v=>Number.isFinite(v)&&v>=0&&v<=1))invalid();
  if(line.a.x===line.b.x&&line.a.y===line.b.y)invalid();
 }
 return next;
}
export function perspectiveGuideStroke(line:PerspectiveLine,rect:{x:number;y:number;width:number;height:number}){
 const ax=rect.x+line.a.x*rect.width,ay=rect.y+line.a.y*rect.height,bx=rect.x+line.b.x*rect.width,by=rect.y+line.b.y*rect.height;
 const length=Math.hypot(bx-ax,by-ay);
 if(![ax,ay,bx,by,length].every(Number.isFinite)||length<=0)throw new Error('PERSPECTIVE_LINE_STROKE_INVALID');
 return {left:(ax+bx)/2-length/2,top:(ay+by)/2-1,width:length,angle:`${Math.atan2(by-ay,bx-ax)}rad`};
}
