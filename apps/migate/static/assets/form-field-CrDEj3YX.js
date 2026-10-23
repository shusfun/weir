import{c as p,j as r}from"./index-1FXVLDCb.js";import{I as u}from"./card-CeyjD7kc.js";import{T as m}from"./textarea-sOvz2_7f.js";import{F as h}from"./field-BQBrlpSX.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const v=p("LockKeyhole",[["circle",{cx:"12",cy:"16",r:"1",key:"1au0dj"}],["rect",{x:"3",y:"10",width:"18",height:"12",rx:"2",key:"6s8ecr"}],["path",{d:"M7 10V7a5 5 0 0 1 10 0v3",key:"1pqi11"}]]);function w({label:c,name:e,type:t="text",value:a,onChange:s,required:i=!0,autoComplete:x,hint:l,className:d}){return r.jsx(h,{label:c,htmlFor:e,hint:l,className:d,children:t==="textarea"?r.jsx(m,{id:e,value:a??"",rows:5,required:i,onChange:o=>s(o.target.value),autoComplete:"off"}):r.jsx(u,{id:e,type:t,value:a??"",onChange:o=>s(o.target.value),required:i,autoComplete:x??(t==="password"?"new-password":void 0)})})}export{v as L,w as T};
