import{c as u,j as o,F as h,T as n,I as p}from"./index-CR--XSVU.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const k=u("LockKeyhole",[["circle",{cx:"12",cy:"16",r:"1",key:"1au0dj"}],["rect",{x:"3",y:"10",width:"18",height:"12",rx:"2",key:"6s8ecr"}],["path",{d:"M7 10V7a5 5 0 0 1 10 0v3",key:"1pqi11"}]]);function v({label:i,name:e,type:t="text",value:r,onChange:s,required:c=!0,autoComplete:x,hint:l,className:d}){return o.jsx(h,{label:i,htmlFor:e,hint:l,className:d,children:t==="textarea"?o.jsx(n,{id:e,value:r??"",rows:5,required:c,onChange:a=>s(a.target.value),autoComplete:"off"}):o.jsx(p,{id:e,type:t,value:r??"",onChange:a=>s(a.target.value),required:c,autoComplete:x??(t==="password"?"new-password":void 0)})})}export{k as L,v as T};
