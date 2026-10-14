import {describe,it,expect} from 'vitest';
import {dragPerspectiveLineEndpoint,perspectiveGuideStroke} from './perspective-lines-drag';
import {perspectiveImageRect} from './perspective-drag';
import type {PerspectiveLine} from './perspective-lines';
const lines=():PerspectiveLine[]=>[{a:{x:0,y:.2},b:{x:1,y:.2},family:'horizontal'},{a:{x:0,y:.8},b:{x:1,y:.8},family:'horizontal'},{a:{x:.2,y:0},b:{x:.2,y:1},family:'vertical'},{a:{x:.8,y:0},b:{x:.8,y:1},family:'vertical'}];
describe('guide line canvas coordinates',()=>{
 it('maps deltas using contained image dimensions and owns endpoints',()=>{
 const original=lines(),rect=perspectiveImageRect(400,280,2);
 expect(rect).toEqual({x:0,y:40,width:400,height:200});
 const moved=dragPerspectiveLineEndpoint(original,0,'a',original[0].a,40,20,rect.width,rect.height);
 expect(moved[0].a.x).toBeCloseTo(.1);expect(moved[0].a.y).toBeCloseTo(.3);expect(original[0].a).toEqual({x:0,y:.2});
 moved[1].b.x=.5;expect(original[1].b.x).toBe(1);
 const clamped=dragPerspectiveLineEndpoint(original,0,'a',original[0].a,-999,999,400,200);expect(clamped[0].a).toEqual({x:0,y:1});
 });
 it('refuses collapsed segments and invalid viewports',()=>{
 const original=lines();expect(()=>dragPerspectiveLineEndpoint(original,0,'a',original[0].a,400,0,400,200)).toThrow();
 expect(()=>dragPerspectiveLineEndpoint(original,0,'a',original[0].a,0,0,0,200)).toThrow();
 expect(()=>dragPerspectiveLineEndpoint(original,0,'a',original[0].a,NaN,0,400,200)).toThrow();
 });
 it('renders centered rotated strokes in the letterboxed image',()=>{
 const rect=perspectiveImageRect(400,280,2),horizontal=perspectiveGuideStroke(lines()[0],rect),vertical=perspectiveGuideStroke(lines()[2],rect);
 expect(horizontal).toEqual({left:0,top:79,width:400,angle:'0rad'});
 expect(vertical.width).toBe(200);expect(vertical.angle).toBe(`${Math.PI/2}rad`);
 });
});
