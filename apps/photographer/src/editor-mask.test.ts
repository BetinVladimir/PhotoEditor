import{describe,expect,it}from"vitest";
import{appendPolygonPoint,appendPromptPoint,appendStrokePoint,finishMaskPolygon,normalizedPromptPoint,startMaskStroke}from"./editor-mask";

describe("editor mask points",()=>{
  it("normalizes taps and preserves positive/negative intent",()=>{
    expect(normalizedPromptPoint(50,25,100,100,"positive")).toEqual({x:.5,y:.25,label:"positive"});
    expect(normalizedPromptPoint(120,-5,100,100,"negative")).toEqual({x:1,y:0,label:"negative"});
  });
  it("bounds the server contract to 128 immutable points",()=>{
    const source=[{x:.5,y:.5,label:"positive" as const}],next=appendPromptPoint(source,{x:.2,y:.3,label:"negative"});
    expect(source).toHaveLength(1);expect(next).toHaveLength(2);
    expect(()=>appendPromptPoint(Array.from({length:128},()=>source[0]),source[0])).toThrow("MASK_POINT_LIMIT");
  });
  it("builds bounded immutable freehand strokes and drops duplicate samples",()=>{
    const first=normalizedPromptPoint(10,20,100,100,"positive"),stroke=startMaskStroke(0,first,"add");
    expect(appendStrokePoint(stroke,{x:.1001,y:.2001})).toBe(stroke);
    const next=appendStrokePoint(stroke,{x:.2,y:.3});expect(stroke.points).toHaveLength(1);expect(next.points).toHaveLength(2);
    expect(()=>startMaskStroke(-1,first,"protect")).toThrow("MASK_STROKE_INVALID");
  });
  it("requires at least three immutable polygon vertices",()=>{const a=appendPolygonPoint([],{x:.1,y:.1}),b=appendPolygonPoint(a,{x:.8,y:.1}),c=appendPolygonPoint(b,{x:.5,y:.8});expect(finishMaskPolygon(2,c,"protect")).toEqual({kind:"polygon",sequence:2,mode:"protect",points:c});expect(()=>finishMaskPolygon(0,b,"add")).toThrow("MASK_POLYGON_INVALID")});
});
