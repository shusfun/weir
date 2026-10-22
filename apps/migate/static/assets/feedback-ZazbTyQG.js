import{c as o,j as t,V as a,B as c}from"./index-O-uNVMZb.js";/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const d=o("RefreshCw",[["path",{d:"M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8",key:"v9h5vc"}],["path",{d:"M21 3v5h-5",key:"1q7to0"}],["path",{d:"M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16",key:"3uifl3"}],["path",{d:"M8 16H3v5",key:"1cv678"}]]);function l({loading:n,error:e,empty:r,onRetry:i,detail:s}){return n?t.jsx(a,{kind:"loading",title:"正在加载数据",detail:"请稍候，正在获取最新状态。"}):e?t.jsx(a,{kind:"error",title:"暂时无法加载",detail:s||e instanceof Error&&e.message||"请检查连接后重试。现有数据不会被更改。",action:i&&t.jsxs(c,{variant:"outline",onClick:i,children:[t.jsx(d,{}),"重新加载"]})}):t.jsx(a,{kind:"empty",title:r??"暂无记录",detail:s})}export{d as R,l as S};
