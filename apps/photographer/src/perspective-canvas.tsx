import {useRef,useState} from 'react';
import {Image,Text,View,type GestureResponderEvent} from 'react-native';
import type {PerspectivePoint} from './perspective-intent';
import {dragPerspectiveCorner,perspectiveImageRect} from './perspective-drag';
type Props={url:string;aspect:number;corners:PerspectivePoint[];disabled:boolean;label:string;onChange:(corners:PerspectivePoint[])=>void};
export function PerspectiveCanvas(p:Props){
 const [size,setSize]=useState({width:0,height:0});
 const drag=useRef<{index:number;point:PerspectivePoint;pageX:number;pageY:number;width:number;height:number}|null>(null);
 const rect=size.width>0&&size.height>0&&p.aspect>0?perspectiveImageRect(size.width,size.height,p.aspect):null;
 function move(event:GestureResponderEvent){
  const current=drag.current;if(!current||p.disabled)return;
  try{p.onChange(dragPerspectiveCorner(p.corners,current.index,current.point,event.nativeEvent.pageX-current.pageX,event.nativeEvent.pageY-current.pageY,current.width,current.height));}catch{/* Keep the last valid convex quad. */}
 }
 return <View onLayout={event=>{drag.current=null;setSize(event.nativeEvent.layout);}} style={{width:'100%',height:240,position:'relative',backgroundColor:'#202124',overflow:'hidden'}}>
 <Image pointerEvents="none" source={{uri:p.url}} resizeMode="contain" style={{width:'100%',height:'100%'}}/>
 {rect?p.corners.map((point,index)=><View key={index} accessibilityLabel={`${p.label} ${index+1}`} onStartShouldSetResponder={()=>!p.disabled} onMoveShouldSetResponder={()=>!p.disabled} onResponderGrant={event=>{drag.current={index,point:{...point},pageX:event.nativeEvent.pageX,pageY:event.nativeEvent.pageY,width:rect.width,height:rect.height};}} onResponderMove={move} onResponderRelease={()=>{drag.current=null;}} onResponderTerminate={()=>{drag.current=null;}} style={{position:'absolute',left:rect.x+point.x*rect.width-22,top:rect.y+point.y*rect.height-22,width:44,height:44,alignItems:'center',justifyContent:'center'}}><View style={{width:24,height:24,borderRadius:12,backgroundColor:p.disabled?'#777':'#f6c344',borderWidth:2,borderColor:'#fff',alignItems:'center',justifyContent:'center'}}><Text>{index+1}</Text></View></View>):null}
 </View>;
}
