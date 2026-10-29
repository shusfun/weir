import{c as j,j as e,o as m,y as w,P as y,I as f}from"./index-DvEEmnfX.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const I=j("LockKeyhole",[["circle",{cx:"12",cy:"16",r:"1",key:"1au0dj"}],["rect",{x:"3",y:"10",width:"18",height:"12",rx:"2",key:"6s8ecr"}],["path",{d:"M7 10V7a5 5 0 0 1 10 0v3",key:"1pqi11"}]]);function L({label:r,name:o,field:c,type:a="text",value:n,onChange:x,required:i=!1,autoComplete:t,hint:d,customError:l,placeholder:u,className:h}){const s={id:o,value:n??"",required:i,placeholder:u,onChange:p=>x(p.target.value)};return e.jsx(m,{label:r,htmlFor:o,name:c,hint:d,customError:l,className:h,children:a==="textarea"?e.jsx(w,{...s,rows:5,autoComplete:"off"}):a==="password"?e.jsx(y,{...s,autoComplete:t??"new-password"}):e.jsx(f,{...s,type:a,autoComplete:t})})}export{I as L,L as T};
