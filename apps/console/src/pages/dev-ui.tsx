import { ThemeControl } from "../theme";
// 动效样例页：只在开发环境注册（app.tsx 路由与 navigation.ts 菜单都在 import.meta.env.DEV 分支里），生产构建不包含。
// 在控制台外壳里打开（侧边栏「开发 → 动效样例」），主题切换用顶栏的。
// 逐条展示 @weir/ui 的 13 种交互反馈，以及表单（校验、确认框、单选、折叠）的写法，用于验收手感与亮暗主题、窄屏、键盘、减少动态效果下的表现。
import { useEffect, useState, type ReactNode } from "react";
import {
  Button,
  Badge,
  ActionGroup,
  CheckboxField,
  Collapsible,
  Field,
  Form,
  FormGrid,
  Input,
  Notice,
  PasswordInput,
  Radio,
  RadioGroup,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  SwitchField,
  useConfirm,
  usePrompt,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardFooter,
  ScrollArea,
  TextLink,
  CardStack,
  Carousel,
  CheckMark,
  Checkbox,
  ChoiceCard,
  CopyButton,
  CopyField,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerTrigger,
  FlipCard,
  FlipCardToggle,
  MenuToggle,
  OptionRow,
  PageHeader,
  Readout,
  SectionHeader,
  SkeletonTable,
  Slider,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  useBurst,
  useToast,
  Chip,
  SplitButton,
  DotCrowd,
  DotMatrix,
  DshFrame,
  FolderSlot,
  GlassFolder,
  GlobeBackground,
  HealthList,
  BulkBar, ChoiceBoard, DraftRecheck, FlowBoard, HoldButton, TrafficUsage, TrafficRing, LaneMap, PhoneFrame, Ticket, TraceSheet, VersionCell,
  InsightBanner,
  MetricStrip,
  MotionProvider,
  Sparkline,
  Squircle,
  Timeline,
  TimelineItem,
  useMotionTier,
} from "@weir/ui";
import { Sparkles } from "lucide-react";

function Demo({ index, title, description, children }: { index: number; title: string; description: string; children: ReactNode }) {
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader title={`${index}. ${title}`} description={description} />
        {children}
      </CardContent>
    </Card>
  );
}

const rows = [
  { name: "东京 01", proto: "VLESS · REALITY", used: "12.4 GB" },
  { name: "香港 02", proto: "Hysteria2", used: "3.1 GB" },
  { name: "新加坡 03", proto: "Trojan · WS", used: "860 MB" },
];

const photos = ["#2f4bd0", "#30a46c", "#e5484d", "#f5a524", "#8e4ec6"].map((color, i) => (
  <svg key={color} viewBox="0 0 400 260" role="img" aria-label={`示例照片 ${i + 1}`}>
    <defs>
      <linearGradient id={`photo-${i}`} x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor={color} />
        <stop offset="1" stopColor="#111318" />
      </linearGradient>
    </defs>
    <rect width="400" height="260" fill={`url(#photo-${i})`} />
    <circle cx={90 + i * 50} cy="90" r="34" fill="rgb(255 255 255 / 0.35)" />
    <path d="M0 220 L120 140 L210 200 L290 120 L400 210 L400 260 L0 260 Z" fill="rgb(0 0 0 / 0.35)" />
    <text x="24" y="240" fill="#fff" fontSize="20" fontWeight="600">照片 {i + 1}</text>
  </svg>
));

const cards = ["订阅源", "手动节点", "订阅组", "分发", "公开页"].map((name, i) => ({ id: i, name }));

export function DevUiPage() {
  const toast = useToast();
  const burst = useBurst();
  const [checked, setChecked] = useState(true);
  const [switchA, setSwitchA] = useState(true);
  const [switchB, setSwitchB] = useState(false);
  const [choice, setChoice] = useState(0);
  const [replay, setReplay] = useState(0);
  const [originDemo, setOriginDemo] = useState(false);
  const [volume, setVolume] = useState(40);
  const [amount, setAmount] = useState(1280);
  const [loading, setLoading] = useState(true);
  const [menu, setMenu] = useState(false);
  const [dialog, setDialog] = useState(false);
  const [longDialog, setLongDialog] = useState(false);
  const [nestedDialog, setNestedDialog] = useState(false);

  return (
    <>
    <GlobeBackground
      nodes={[
        { id: "sh", lat: 31.2, lon: 121.5, tone: "ok" },
        { id: "la", lat: 34.05, lon: -118.2, tone: "bad" },
        { id: "gap", lat: Number.NaN, lon: 139.7, tone: "unknown" },
      ]}
      links={[{ from: "sh", to: "la", tone: "info" }]}
    />
    <div className="weir-above-globe stack">
        <ThemeControl />
      <PageHeader title="动效样例" description="@weir/ui 的交互反馈。右下角地球用的是样例坐标，不是线上节点；登录页不要传入真实节点。手机宽度不渲染地球。" />
      <Card variant="inset"><CardHeader title="公共卡片标题" description="标题和说明共用边线，操作区独立。" actions={<Button variant="outline">操作</Button>} /><CardContent className="stack"><ActionGroup><Button>主要操作</Button><CopyButton variant="outline" value="Weir UI">复制示例</CopyButton><Button variant="ghost">次要操作</Button></ActionGroup><ActionGroup><Badge>强调标签</Badge><Badge variant="soft" tone="neutral">中性标签</Badge></ActionGroup></CardContent></Card>

        <Card size="lg">
          <CardHeader><CardTitle>大号卡片对齐</CardTitle></CardHeader>
          <CardContent>标题、正文与底栏共用横向内边距。</CardContent>
          <CardFooter><Button variant="outline">底栏操作</Button></CardFooter>
        </Card>
        <div className="grid gap-4 lg:grid-cols-2">
          <Demo index={1} title="按压回弹" description="按下下沉并缩小，松手冲过原位再落回。">
            <div className="actions">
              <Button>主操作</Button>
              <Button variant="outline">次要操作</Button>
              <Button variant="ghost">幽灵键</Button>
              <Button variant="secondary">已选中</Button>
              {(["xs", "sm", "default", "lg"] as const).map(size => <Button key={size} size={size} variant="outline">{size}</Button>)}
              {(["icon-xs", "icon-sm", "icon", "icon-lg"] as const).map(size => <Button key={size} size={size} variant="outline" aria-label={size}><Sparkles /></Button>)}
              <Button busy={loading} busyText="正在保存" onClick={() => setLoading(true)}>保存设置</Button>
              <Button variant="link" onClick={() => setLoading(v => !v)}>切换忙碌状态</Button>
            </div>
          </Demo>

          <Demo index={2} title="弹性开关" description="按住拨钮横向拉长；切换时行进中保持拉长，落位压回原宽。">
            <div className="flex flex-wrap gap-x-6">
              <SwitchField label="默认尺寸" checked={switchA} onCheckedChange={setSwitchA} />
              <SwitchField label="小尺寸" size="sm" checked={switchB} onCheckedChange={setSwitchB} />
            </div>
          </Demo>

          <Demo index={3} title="按压反馈" description="按下缩到 0.97 并叠一层填充色，松手按弹簧回弹；减少动态效果时只变颜色。">
            <Button size="lg" className="w-full">
              大面积主操作
            </Button>
            <div className="grid gap-3 sm:grid-cols-2">
              {["SQLite 单机", "PostgreSQL + Redis"].map((name, i) => (
                <ChoiceCard key={name} selected={choice === i} render={<button type="button" />} onClick={() => setChoice(i)} className="text-left">
                  <strong>{name}</strong>
                  <small className="text-[var(--weir-ink-3)]">点击卡片任意位置</small>
                </ChoiceCard>
              ))}
            </div>
            <OptionRow>
              <Checkbox defaultChecked />
              <span>
                <strong>勾选行</strong>
                <small className="text-[var(--weir-ink-3)]">整行可点，按下叠填充色</small>
              </span>
            </OptionRow>
          </Demo>

          <Demo index={4} title="点击爆散" description="只留给关键的那一下，例如安装完成。">
            <Button onClick={(e) => burst(e.nativeEvent)}>
              <Sparkles />
              完成关键操作
            </Button>
          </Demo>

          <Demo index={5} title="成功勾选" description="对勾沿路径一笔画出；首屏已勾选的直接显示。复制：复制图标缩小淡出、对勾放大并描绘。">
            <CopyField label="订阅地址" value="https://example.com/sub/demo" />
            <div className="actions">
              <CopyButton variant="outline" value="weir">复制示例文本</CopyButton>
              <CopyButton value="weir" aria-label="复制示例文本" />
              <CheckboxField label="复选框" checked={checked} onCheckedChange={setChecked} />
              <Button variant="outline" onClick={() => toast("已保存", { description: "配置已下发到 3 台 VPS。" })}>成功提示</Button>
            </div>
            <div className="actions">
              <span className="text-[var(--weir-ok)] [&>svg]:size-10">
                <CheckMark key={replay} circle />
              </span>
              <Button variant="ghost" size="sm" onClick={() => setReplay((v) => v + 1)}>重播</Button>
            </div>
          </Demo>

          <Demo index={6} title="液态滑块" description="填充、滑钮、数值同频移动；拖动时直接跟手。">
            <div className="pt-8">
              <Slider aria-label="音量" value={volume} onValueChange={(v) => setVolume(v as number)} />
            </div>
            <div className="actions">
              <Button variant="outline" size="sm" onClick={() => setVolume(10)}>10</Button>
              <Button variant="outline" size="sm" onClick={() => setVolume(90)}>90</Button>
            </div>
          </Demo>

          <Demo index={7} title="数字滚动" description="所有位同时出发、同时慢慢停下。">
            <Readout label="本月流量" value={amount} unit="GB" framed />
            <div className="actions">
              <Button variant="outline" size="sm" onClick={() => setAmount((v) => v + Math.ceil(Math.random() * 400))}>增加</Button>
              <Button variant="outline" size="sm" onClick={() => setAmount((v) => Math.max(0, v - Math.ceil(Math.random() * 400)))}>减少</Button>
            </div>
          </Demo>

          <Demo index={8} title="骨架落位" description="骨架在真实表格里，行高与数据一致，切换时不跳。">
            <Button variant="outline" size="sm" className="self-start" onClick={() => setLoading((v) => !v)}>
              {loading ? "显示数据" : "显示骨架"}
            </Button>
            {loading ? (
              <SkeletonTable columns={["节点", "协议", "用量"]} rows={3} />
            ) : (
              <Card>
                <CardContent>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>节点</TableHead>
                        <TableHead>协议</TableHead>
                        <TableHead>用量</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {rows.map((r) => (
                        <TableRow key={r.name}>
                          <TableCell>{r.name}</TableCell>
                          <TableCell>{r.proto}</TableCell>
                          <TableCell>{r.used}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            )}
          </Demo>

          <Demo index={9} title="卡片翻面" description="两面各自藏好背面；背向的一面不可聚焦。">
            <FlipCard
              front={
                <div className="weir-panel stack p-5">
                  <strong>订阅地址</strong>
                  <small className="text-[var(--weir-ink-3)]">翻到背面查看二维码说明</small>
                  <FlipCardToggle className="justify-self-start">查看背面</FlipCardToggle>
                </div>
              }
              back={
                <div className="weir-panel stack p-5">
                  <strong>二维码</strong>
                  <small className="text-[var(--weir-ink-3)]">用客户端扫码导入，地址只显示一次。</small>
                  <FlipCardToggle className="justify-self-start">返回正面</FlipCardToggle>
                </div>
              }
            />
          </Demo>

          <Demo index={10} title="汉堡变叉" description="同一个元件从三条线走到叉，再倒序走回。">
            <div className="actions">
              <Button variant="outline" size="icon" aria-label={menu ? "关闭菜单" : "打开菜单"} aria-expanded={menu} onClick={() => setMenu((v) => !v)}>
                <MenuToggle open={menu} />
              </Button>
              <small className="text-[var(--weir-ink-3)]">移动端导航（宽度 ≤ 900px）使用同一个元件。</small>
            </div>
          </Demo>

          <Demo index={11} title="卡片抽走" description="拖走顶卡或按方向键；后面的卡露出边缘并依次补位。">
            <CardStack
              items={cards}
              getKey={(c) => c.id}
              label="功能卡片"
              renderItem={(c) => (
                <div className="stack p-5">
                  <strong>{c.name}</strong>
                  <small className="text-[var(--weir-ink-3)]">向左或向右拖动，甩出即可切到下一张。</small>
                </div>
              )}
            />
          </Demo>

          <Demo index={12} title="底部抽屉" description="升到位后停顿一拍，内容再落稳；可下滑关闭。窄屏下普通弹窗也从底部升起。">
            <div className="actions">
              <Drawer>
                <DrawerTrigger render={<Button variant="outline" />}>打开底部抽屉</DrawerTrigger>
                <DrawerContent
                  title="分享订阅组"
                  description="拖动顶部把手向下滑即可关闭。"
                  footer={<DrawerClose render={<Button />}>完成</DrawerClose>}
                >
                  <p className="text-[var(--weir-ink-2)]">匿名访客通过分享链接只能看到订阅组内的节点，不包含用户账号信息。</p>
                </DrawerContent>
              </Drawer>
              <Button variant="outline" onClick={() => setDialog(true)}>打开普通弹窗</Button>
              <Button variant="outline" onClick={() => setLongDialog(true)}>打开长表单弹窗</Button>
            </div>
            <Dialog open={longDialog} onOpenChange={setLongDialog}>
              <DialogContent size="lg">
                <DialogTitle>长表单与固定底栏</DialogTitle>
                <DialogDescription>正文独立滚动，轨道与字段、焦点环分别占位。</DialogDescription>
                <Form onSubmit={() => setNestedDialog(true)}>
                  <Button variant="outline" onClick={() => setNestedDialog(true)}>打开二级弹窗</Button>
                  {Array.from({ length: 18 }, (_, i) => <Field key={i} label={`示例字段 ${i + 1}`}><Input defaultValue={`内容 ${i + 1}`} /></Field>)}
                  <ScrollArea orientation="both" gutter="stable" className="h-[calc(var(--weir-gap-41)*3)]">
                    <div className="w-max whitespace-pre p-[var(--weir-space-2)] font-mono">{Array.from({ length: 12 }, (_, i) => `示例 ${i + 1}：${"横向内容 ".repeat(20)}`).join("\n")}</div>
                  </ScrollArea>
                  <DialogFooter><Button type="submit">检查并继续</Button></DialogFooter>
                </Form>
                <Dialog open={nestedDialog} onOpenChange={setNestedDialog}>
                  <DialogContent><DialogTitle>二级确认</DialogTitle><DialogDescription>关闭后回到长表单原来的滚动位置。</DialogDescription><DialogFooter><Button onClick={() => setNestedDialog(false)}>返回表单</Button></DialogFooter></DialogContent>
                </Dialog>
              </DialogContent>
            </Dialog>
            <Dialog open={dialog} onOpenChange={setDialog}>
              <DialogContent>
                <DialogTitle>普通弹窗</DialogTitle>
                <DialogDescription>宽度 640px 以下改为底部呈现，接口和焦点行为不变。</DialogDescription>
                <DialogFooter>
                  <Button variant="outline" onClick={() => setDialog(false)}>关闭</Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </Demo>

          <Demo index={13} title="照片跟手" description="拖动时照片跟着手指转起来，松手按速度吸附到最近一张。">
            <Carousel slides={photos} label="示例照片" />
          </Demo>

          <Demo index={14} title="从点击处展开" description="标签键、分体按钮按下缩放并叠填充；点开的弹窗从按下的位置展开、收回到那里。">
            <div className="flex flex-wrap gap-2">
              <Chip tone="ok" meta="在线" onClick={() => setOriginDemo(true)}>VLESS + REALITY</Chip>
              <Chip tone="bad" meta="连不上" onClick={() => setOriginDemo(true)}>Hysteria2</Chip>
              <Chip add onClick={() => setOriginDemo(true)}>＋ 协议</Chip>
            </div>
            <SplitButton
              main={<CopyButton size="sm" variant="outline" value="https://example.com/sub/demo">复制</CopyButton>}
              items={[{ label: "二维码", onSelect: () => setOriginDemo(true) }, "separator", { label: "删除", danger: true, onSelect: () => toast("已删除") }]}
            />
            <Dialog open={originDemo} onOpenChange={setOriginDemo}>
              <DialogContent size="sm">
                <DialogTitle>从这里展开</DialogTitle>
                <DialogDescription>关闭时收回到刚才按下的位置。</DialogDescription>
              </DialogContent>
            </Dialog>
          </Demo>
        </div>
        <FormDemo />
        <MaterialDemos />
    </div>
    </>
  );
}

// 样例坐标不是线上节点。登录页不要把真实节点传给 GlobeBackground。
function MaterialDemos() {
  const toast = useToast();
  const tier = useMotionTier();
  const [picked, setPicked] = useState("甲");
  const [dsh, setDsh] = useState(false);
  const [trace, setTrace] = useState(false);
  useEffect(() => {
    const prev = document.documentElement.dataset.motion;
    return () => {
      if (prev == null) delete document.documentElement.dataset.motion;
      else document.documentElement.dataset.motion = prev;
    };
  }, []);
  function setTier(next: "full" | "balanced" | "lite" | "system") {
    if (next === "system") delete document.documentElement.dataset.motion;
    else document.documentElement.dataset.motion = next;
  }
  return (
    <div className="grid gap-[var(--weir-space-4)] lg:grid-cols-2">
      <Demo index={16} title="动效档位" description="当前档位来自 useMotionTier。省电档停掉持续动画。离开本页会还原原来的档位。">
        <p className="m-0 text-body">当前：{tier}</p>
        <div className="actions">
          {(["full", "balanced", "lite", "system"] as const).map((name) => (
            <Button key={name} type="button" variant="outline" size="sm" onClick={() => setTier(name)}>{name === "system" ? "跟随系统" : name}</Button>
          ))}
        </div>
        <MotionProvider tier="lite">
          <p className="m-0 text-meta text-[var(--weir-ink-3)]">子树覆盖为省电，不影响上面的读数。</p>
          <TierEcho />
        </MotionProvider>
      </Demo>

      <Demo index={17} title="关键指标与走势" description="缺数显示未知，走势断开处不连成 0。入场描线只在完整档。">
        <MetricStrip items={[
          { label: "在线", value: 2, unit: "台", delta: { text: "1", direction: "up", better: "up" } },
          { label: "24 小时重启", value: null, unit: "次", delta: null },
          { label: "待确认", value: 1, caption: "草稿" },
        ]} />
        <div className="actions">
          <Sparkline label="样例延迟" tone="info" values={[12, 18, null, 9, 14, 11]} />
          <Sparkline label="没有样本" values={null} />
        </div>
      </Demo>

      <Demo index={18} title="点阵与群像" description="悬停能看到是谁。点一下定位到下面的名字。流量未知不用 0 代替。">
        <DotMatrix label="本月额度" value={13} max={21} />
        <DotMatrix label="昨日流量" value={null} max={null} />
        <DotCrowd
          members={[
            { id: "甲", name: "甲", weight: 0.8, tone: "ok" },
            { id: "乙", name: "乙", weight: 0.2, tone: "warn" },
            { id: "丙", name: "丙", weight: null, tone: "unknown" },
          ]}
          onSelect={setPicked}
        />
        <p className="m-0 text-body">当前定位：{picked}</p>
      </Demo>

      <Demo index={19} title="玻璃文件夹与超椭圆" description="整块可点。地址缺失显示未知。前板只在完整档磨砂。">
        <GlassFolder
          name="样例 VPS"
          address={null}
          status={{ tone: "bad", text: "DNS 被污染" }}
          cpu={22}
          caption="CPU 22% · 2 个节点"
          metric={null}
          metricUnit="ms"
          metricLabel="国内延迟"
          files={[{ id: "a", title: "VLESS", meta: "43512", tone: "ok" }, { id: "b", title: null, tone: "unknown" }]}
          onClick={() => toast("打开样例详情")}
        />
        <GlassFolder name="另一台" hue={1} address="203.0.113.9" cpu={null} caption="CPU 未知 · 0 个节点" files={[]} />
        <FolderSlot title="添加 VPS" caption="用 SSH 一次装好" onClick={() => toast("打开添加 VPS")} />
        <Squircle path className="px-[var(--weir-space-4)] py-[var(--weir-space-3)]">
          <strong>路径超椭圆</strong>
          <p className="m-0 text-meta text-[var(--weir-ink-3)]">描边和投影跟着路径。</p>
        </Squircle>
      </Demo>

      <Demo index={20} title="时间线与洞察" description="重启用故障色，Laya 用强调色。缺记录不写成正常。">
        <InsightBanner
          title="样例节点的证书快要到期"
          confidence={null}
          progress="已交给 dsh"
          more={1}
          trace={<TextLink render={<a href="#trace" />}>trace</TextLink>}
          action={<Button size="sm" variant="outline" onClick={() => toast("查看草稿")}>查看草稿</Button>}
        />
        <Notice tone="insight">短洞察也可以用 Notice 的 insight。</Notice>
        <Timeline>
          <TimelineItem time="09:41:02" tone="restart" title="内核整进程重启" description="配置版本 18 · 证书轮换" tags="断开 2 条" trace={<TextLink render={<a href="#trace" />}>trace</TextLink>} />
          <TimelineItem time={null} tone="laya" title="Laya 判断不需要改" description="置信度未知" />
          <TimelineItem time="10:02:11" tone="hot" title="热更新，未重启" />
        </Timeline>
      </Demo>

      <Demo index={21} title="体检、按住确认、dsh 外框" description="按住按钮或按住回车才会触发。外框可以拖、改大小、缩成胶囊。">
        <HealthList items={[
          { id: "dns", name: "系统 DNS", status: "ok", label: "实测通过" },
          { id: "cert", name: "证书", status: "warn", label: "9 天后到期", onHandle: () => toast("去处理证书") },
          { id: "cpu", name: "宿主机 CPU", status: null, label: null },
        ]} />
        <div className="flex flex-wrap items-center gap-3"><Button variant="ghost">稍后</Button><Button variant="outline">放弃</Button><HoldButton onHold={() => toast("已确认", { tone: "ok" })}>按住确认执行</HoldButton></div>
        <div className="grid gap-3 sm:grid-cols-3">{[{ used: 0, quota: 100 }, { used: 95, quota: 100 }, { used: 120, quota: 100 }, { used: null, quota: null }, { used: 1, quota: 0 }, { used: 5, quota: 0, unlimited: true }].map((v, i) => <TrafficUsage key={i} {...v} />)}</div>
        <TrafficRing used={40.1 * 2 ** 30} quota={42 * 2 ** 30} />
        <HoldButton onHold={() => toast("已确认执行", { tone: "ok" })}>按住确认执行</HoldButton>
        <Button type="button" variant="outline" onClick={() => setDsh(true)}>打开 dsh 外框</Button>
        {dsh && (
          <DshFrame pageLabel={null} draftCount={1} ready={false} progress={null} onClose={() => setDsh(false)} steps={[
            { label: "下载 dsh", status: "done" },
            { label: "等它启动", status: "current", caption: "已等 7 秒" },
            { label: "填写 API Key", status: "upcoming" },
          ]}>
            <p className="m-0 text-[var(--weir-ink-3)]">这里只留给 dsh 自己的界面。</p>
          </DshFrame>
        )}
      </Demo>

      <Demo index={22} title="画板部件" description="流水线、轨道、票据、预览框、批量条和版本格。样例只在这一页。">
        <FlowBoard label="样例流水线" steps={[{ label: "订阅源", value: 2, caption: "上游" }, { label: "订阅组", value: null, caption: "数量未知" }]} />
        <LaneMap label="样例轨道" lanes={[{ id: "1", from: "样例规则", to: "样例出口" }]} />
        <ChoiceBoard label="样例选择"><Ticket name="样例链接" meta="使用者未知" url={null} note="最近 21 天未知" /></ChoiceBoard>
        <PhoneFrame title="样例预览" fallback={<p>没有公开地址</p>} />
        <BulkBar count={2}><Button size="sm" variant="outline">样例操作</Button></BulkBar>
        <p className="m-0">版本 <VersionCell version="" /> / <VersionCell version={null} /></p>
        <DraftRecheck blocked detail="样例：只有真的拦下才在业务页出现。" />
        <Button type="button" variant="outline" onClick={() => setTrace(true)}>打开断线侧栏</Button>
        <TraceSheet open={trace} onOpenChange={setTrace}>
          <Timeline><TimelineItem time={null} tone="unknown" title="环节未知" /></Timeline>
        </TraceSheet>
      </Demo>
    </div>
  );
}

function TierEcho() {
  const tier = useMotionTier();
  return <p className="m-0 text-body">子树档位：{tier}</p>;
}

// 表单样例：必填 / 格式 / 跨字段规则在字段下方提示；邮箱填 taken@example.com 模拟服务端返回的字段错误。
function FormDemo() {
  const toast = useToast();
  const confirm = useConfirm();
  const prompt = usePrompt();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");
  const [region, setRegion] = useState("");
  const [plan, setPlan] = useState("basic");
  const [notify, setNotify] = useState(true);
  const [agree, setAgree] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>();
  return (
    <Demo index={15} title="表单校验" description="参照 Element Plus：必填项带红色 *，失焦校验，出错后随输入实时更新；提交时聚焦第一个错误。错误行预留空间，不推动布局。">
      <Form
        errors={errors}
        onSubmit={() => {
          if (email === "taken@example.com") return setErrors({ email: "该邮箱已被使用。" });
          setErrors(undefined);
          toast("校验通过", { tone: "ok" });
        }}
      >
        <FormGrid>
          <Field label="邮箱" name="email" hint="填 taken@example.com 试试服务端错误。">
            <Input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label="地区">
            <Select required value={region} onValueChange={(v) => setRegion(String(v ?? ""))}>
              <SelectTrigger><SelectValue>{region || "请选择"}</SelectValue></SelectTrigger>
              <SelectContent>
                {["东京", "香港", "新加坡"].map((r) => <SelectItem key={r} value={r}>{r}</SelectItem>)}
              </SelectContent>
            </Select>
          </Field>
          <Field label="密码" hint="至少 6 位。">
            <PasswordInput required minLength={6} autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Field label="确认密码" customError={repeat && repeat !== password ? "两次输入的密码不一致" : ""}>
            <PasswordInput required autoComplete="new-password" value={repeat} onChange={(e) => setRepeat(e.target.value)} />
          </Field>
        </FormGrid>
        <RadioGroup aria-label="套餐" className="flex flex-wrap gap-4" value={plan} onValueChange={(v) => setPlan(String(v))}>
          {[["basic", "基础"], ["pro", "专业"], ["team", "团队"]].map(([v, n]) => (
            <label key={v} className="weir-press-fill flex min-h-11 items-center gap-3 rounded-[var(--weir-radius-sm)] px-2 text-body"><Radio value={v} />{n}</label>
          ))}
        </RadioGroup>
        <SwitchField label="接收通知" description="开关与说明整行可点。" checked={notify} onCheckedChange={setNotify} />
        <Field customError={agree ? "" : "请先阅读并同意"}>
          <CheckboxField label="我已阅读使用说明" checked={agree} onCheckedChange={setAgree} />
        </Field>
        <Collapsible title="高级选项">
          <Notice>折叠区内容。展开 / 收起有高度过渡。</Notice>
        </Collapsible>
        <div className="actions">
          <Button type="submit">提交</Button>
          <Button type="button" variant="outline" onClick={async () => toast((await confirm({ title: "删除这个节点？", description: "使用它的订阅会立即失效。", confirmText: "删除", tone: "danger" })) ? "已确认" : "已取消")}>确认框</Button>
          <Button type="button" variant="outline" onClick={async () => { const v = await prompt({ title: "重命名分组", label: "新分组名", defaultValue: "香港", required: true }); toast(v === null ? "已取消" : "新名称：" + v); }}>输入框</Button>
        </div>
      </Form>
    </Demo>
  );
}
