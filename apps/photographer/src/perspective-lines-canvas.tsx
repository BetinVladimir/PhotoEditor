import {useRef,useState} from 'react';
import {Image,Text,View,type GestureResponderEvent} from 'react-native';
import type {PerspectiveLine} from './perspective-lines';
import type {PerspectivePoint} from './perspective-intent';
import {perspectiveImageRect} from './perspective-drag';
import {dragPerspectiveLineEndpoint,perspectiveGuideStroke} from './perspective-lines-drag';
type Props={url:string;aspect:number;lines:PerspectiveLine[];disabled:boolean;horizontalLabel:string;verticalLabel:string;onChange:(lines:PerspectiveLine[])=>void};
export function PerspectiveLinesCanvas(p:Props){
 const [size,setSize]=useState({width:0,height:0});
 const drag=useRef<{index:number;endpoint:'a'|'b';point:PerspectivePoint;pageX:number;pageY:number;width:number;height:number}|null>(null);
 const rect=size.width>0&&size.height>0&&Number.isFinite(p.aspect)&&p.aspect>0?perspectiveImageRect(size.width,size.height,p.aspect):null;
 function move(event:GestureResponderEvent){const start=drag.current;if(!start||p.disabled)return;try{p.onChange(dragPerspectiveLineEndpoint(p.lines,start.index,start.endpoint,start.point,event.nativeEvent.pageX-start.pageX,event.nativeEvent.pageY-start.pageY,start.width,start.height));}catch{/* Preserve the last valid nonzero segment. */}}
 return <View onLayout={event=>{drag.current=null;setSize(event.nativeEvent.layout);}} style={{width:'100%',height:280,position:'relative',backgroundColor:'#202124',overflow:'hidden'}}>
 <Image pointerEvents="none" source={{uri:p.url}} resizeMode="contain" style={{width:'100%',height:'100%'}}/>
 {rect?p.lines.map((line,index)=>{let stroke:ReturnType<typeof perspectiveGuideStroke>|null=null;try{stroke=perspectiveGuideStroke(line,rect);}catch{}const color=line.family==='horizontal'?'#f6c344':'#61dafb';return <View key={index} pointerEvents="box-none" style={{position:'absolute',left:0,right:0,top:0,bottom:0}}>
 {stroke?<View pointerEvents="none" style={{position:'absolute',left:stroke.left,top:stroke.top,width:stroke.width,height:2,backgroundColor:color,transform:[{rotate:stroke.angle}]}}/>:null}
 {(['a','b'] as const).map(endpoint=><View key={endpoint} accessibilityLabel={`${line.family==='horizontal'?p.horizontalLabel:p.verticalLabel} ${index%2+1} ${endpoint}`} onStartShouldSetResponder={()=>!p.disabled} onMoveShouldSetResponder={()=>!p.disabled} onResponderGrant={event=>{drag.current={index,endpoint,point:{...line[endpoint]},pageX:event.nativeEvent.pageX,pageY:event.nativeEvent.pageY,width:rect.width,height:rect.height};}} onResponderMove={move} onResponderRelease={()=>{drag.current=null;}} onResponderTerminate={()=>{drag.current=null;}} style={{position:'absolute',left:rect.x+line[endpoint].x*rect.width-22,top:rect.y+line[endpoint].y*rect.height-22,width:44,height:44,alignItems:'center',justifyContent:'center'}}><View style={{width:26,height:26,borderRadius:13,backgroundColor:p.disabled?'#777':color,borderWidth:2,borderColor:'#fff',alignItems:'center',justifyContent:'center'}}><Text>{index%2+1}{endpoint}</Text></View></View>)}
 </View>}):null}
 </View>;
}
