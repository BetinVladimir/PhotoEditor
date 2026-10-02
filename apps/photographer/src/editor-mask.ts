export type PromptPoint={x:number;y:number;label:"positive"|"negative"};
export type MaskStroke={kind:"stroke";sequence:number;mode:"add"|"subtract"|"protect";points:Array<{x:number;y:number}>;radius:number;hardness:number};
export type MaskPolygon={kind:"polygon";sequence:number;mode:"add"|"subtract"|"protect";points:Array<{x:number;y:number}>};

export function normalizedPromptPoint(locationX:number,locationY:number,width:number,height:number,label:PromptPoint["label"]):PromptPoint{
  if(![locationX,locationY,width,height].every(Number.isFinite)||width<=0||height<=0)throw new Error("MASK_POINT_INVALID");
  const x=Math.min(1,Math.max(0,locationX/width)),y=Math.min(1,Math.max(0,locationY/height));
  return{x:Number(x.toFixed(6)),y:Number(y.toFixed(6)),label};
}

export function appendPromptPoint(points:PromptPoint[],point:PromptPoint,max=128):PromptPoint[]{
  if(points.length>=max)throw new Error("MASK_POINT_LIMIT");
  return[...points,point];
}

export function startMaskStroke(sequence:number,point:{x:number;y:number},mode:MaskStroke["mode"],radius=.025,hardness=.8):MaskStroke{
  if(!Number.isInteger(sequence)||sequence<0||sequence>2147483647||radius<=0||radius>1||hardness<0||hardness>1)throw new Error("MASK_STROKE_INVALID");
  return{kind:"stroke",sequence,mode,points:[point],radius,hardness};
}

export function appendStrokePoint(stroke:MaskStroke,point:{x:number;y:number},max=4096):MaskStroke{
  if(stroke.points.length>=max)throw new Error("MASK_STROKE_LIMIT");
  const last=stroke.points[stroke.points.length-1];
  if(last&&Math.hypot(point.x-last.x,point.y-last.y)<.001)return stroke;
  return{...stroke,points:[...stroke.points,point]};
}
export function appendPolygonPoint(points:Array<{x:number;y:number}>,point:{x:number;y:number},max=4096){if(points.length>=max)throw new Error("MASK_POLYGON_LIMIT");return[...points,point]}
export function finishMaskPolygon(sequence:number,points:Array<{x:number;y:number}>,mode:MaskPolygon["mode"]):MaskPolygon{if(!Number.isInteger(sequence)||sequence<0||sequence>2147483647||points.length<3||points.length>4096)throw new Error("MASK_POLYGON_INVALID");return{kind:"polygon",sequence,mode,points:[...points]}}
