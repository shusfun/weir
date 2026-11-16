// 交互整理原型（只在开发环境）：/dev/proto 下用现有组件和模拟数据拼出新的页面结构，确认后再改正式页面。
// 不接接口、不需要登录；生产构建里 app.tsx 不引用本目录。顶栏的身份切换只为演示两种账户看到什么。
import { Route, Routes } from "react-router-dom";
import { ProtoShell } from "./parts";
import { ProtoOverview } from "./overview";
import { ProtoAllNodes, ProtoKernels, ProtoMyNodes, ProtoVps } from "./nodes";
import { ProtoProxies } from "./proxies";
import { ProtoRules } from "./rules";
import { ProtoSources } from "./sources";
import { ProtoStub, ProtoSubs } from "./subs";
import { ProtoPlans, ProtoUsers } from "./users";
import { ProtoMe, ProtoSystem } from "./system";

export function ProtoApp() {
  return (
    <ProtoShell>
      <Routes>
        <Route path="/dev/proto" element={<ProtoOverview />} />
        <Route path="/dev/proto/nodes" element={<ProtoVps />} />
        <Route path="/dev/proto/nodes/all" element={<ProtoAllNodes />} />
        <Route path="/dev/proto/proxies" element={<ProtoProxies />} />
        <Route path="/dev/proto/rules" element={<ProtoRules />} />
        <Route path="/dev/proto/kernels" element={<ProtoKernels />} />
        <Route path="/dev/proto/users" element={<ProtoUsers />} />
        <Route path="/dev/proto/plans" element={<ProtoPlans />} />
        <Route path="/dev/proto/subs" element={<ProtoSubs />} />
        <Route path="/dev/proto/sources" element={<ProtoSources />} />
        <Route path="/dev/proto/groups" element={<ProtoStub eyebrow="订阅" title="订阅组与模板" note="沿用现有订阅组与规则模板页面，合到一处。" />} />
        <Route path="/dev/proto/pages" element={<ProtoStub eyebrow="订阅" title="公开页面" />} />
        <Route path="/dev/proto/observe" element={<ProtoStub eyebrow="运维" title="运行观测" note="单独一页；状态与原因改为后端的唯一结论，内部代码只进详情。" />} />
        <Route path="/dev/proto/audit" element={<ProtoStub eyebrow="运维" title="操作审计" note="单独一页；用户详情里按用户筛出的操作记录也来自这里。" />} />
        <Route path="/dev/proto/system" element={<ProtoSystem />} />
        <Route path="/dev/proto/my/nodes" element={<ProtoMyNodes />} />
        <Route path="/dev/proto/me" element={<ProtoMe />} />
        <Route path="*" element={<ProtoOverview />} />
      </Routes>
    </ProtoShell>
  );
}
