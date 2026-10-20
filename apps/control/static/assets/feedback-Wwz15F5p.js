import{c as e,j as s,B as l,v as o}from"./index-Cal28upz.js";import{l as r}from"./models-BwWGssbO.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const x=e("CircleAlert",[["circle",{cx:"12",cy:"12",r:"10",key:"1mglay"}],["line",{x1:"12",x2:"12",y1:"8",y2:"12",key:"1pkeuh"}],["line",{x1:"12",x2:"12.01",y1:"16",y2:"16",key:"4dfq90"}]]);/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const d=e("Inbox",[["polyline",{points:"22 12 16 12 14 15 10 15 8 12 2 12",key:"o97t9d"}],["path",{d:"M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z",key:"oot6mr"}]]);/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const p=e("LoaderCircle",[["path",{d:"M21 12a9 9 0 1 1-6.219-8.56",key:"13zald"}]]);function j({loading:t,error:a,empty:i,onRetry:n,detail:c}){return s.jsxs("div",{className:"page-state"+(a?" error":""),role:a?"alert":"status",children:[t?s.jsx(p,{className:"spin",size:24}):a?s.jsx(x,{size:28}):s.jsx(d,{size:30}),s.jsx("strong",{children:t?"正在加载数据":a?"暂时无法加载":i??"暂无记录"}),s.jsx("p",{children:a?c||"请检查连接后重试。现有数据不会被更改。":t?"请稍候，正在获取最新状态。":c??"创建记录后会显示在这里。"}),!!a&&n&&s.jsx(l,{variant:"outline",onClick:n,children:"重新加载"})]})}function m({value:t}){return s.jsxs(o,{className:"status-badge status-"+t,variant:"outline",children:[s.jsx("span",{className:"status-dot"}),r[t]??t]})}export{d as I,j as S,m as a};
