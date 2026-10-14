import {it,expect} from 'vitest';
import {dragPerspectiveCorner,perspectiveImageRect} from './perspective-drag';
const corners=()=>[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}];
it('maps contain layouts and gesture deltas into source coordinates',()=>{
 expect(perspectiveImageRect(400,400,2)).toEqual({x:0,y:100,width:400,height:200});
 expect(perspectiveImageRect(400,200,.5)).toEqual({x:150,y:0,width:100,height:200});
 const original=corners();const moved=dragPerspectiveCorner(original,0,original[0],40,20,400,200);
 expect(moved[0]).toEqual({x:.1,y:.1});expect(original[0]).toEqual({x:0,y:0});
 expect(dragPerspectiveCorner(original,0,original[0],-100,-100,400,200)[0]).toEqual({x:0,y:0});
});
it('refuses corner crossings and invalid geometry',()=>{
 expect(()=>dragPerspectiveCorner(corners(),0,{x:0,y:0},400,200,400,200)).toThrow();
 expect(()=>dragPerspectiveCorner(corners(),0,{x:0,y:0},1,1,0,200)).toThrow();
 expect(()=>perspectiveImageRect(1,1,NaN)).toThrow();
});
