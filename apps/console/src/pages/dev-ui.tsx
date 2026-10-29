// 动效样例页：只在开发环境注册（app.tsx 路由与 navigation.ts 菜单都在 import.meta.env.DEV 分支里），生产构建不包含。
// 在控制台外壳里打开（侧边栏「开发 → 动效样例」），主题切换用顶栏的。
// 逐条展示 @weir/ui 的 13 种交互反馈，以及表单（校验、确认框、单选、折叠）的写法，用于验收手感与亮暗主题、窄屏、键盘、减少动态效果下的表现。
import { useState, type ReactNode } from "react";
import {
  Button,
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
  CardStack,
  Carousel,
  CheckMark,
  Checkbox,
  ChoiceCard,
  CopyButton,
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
  Label,
  MenuToggle,
  OptionRow,
  PageHeader,
  Readout,
  SectionHeader,
  SkeletonTable,
  Slider,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  useBurst,
  useToast,
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
  const [choice, setChoice] = useState(0);
  const [replay, setReplay] = useState(0);
  const [volume, setVolume] = useState(40);
  const [amount, setAmount] = useState(1280);
  const [loading, setLoading] = useState(true);
  const [menu, setMenu] = useState(false);
  const [dialog, setDialog] = useState(false);

  return (
    <div className="stack">
        <PageHeader eyebrow="开发" title="动效样例" description="@weir/ui 的交互反馈。窄屏、暗色、键盘操作和系统“减少动态效果”下都应可用。" />
        <div className="grid gap-4 lg:grid-cols-2">
          <Demo index={1} title="按压回弹" description="按下下沉并缩小，松手冲过原位再落回。">
            <div className="actions">
              <Button>主操作</Button>
              <Button variant="outline">次要操作</Button>
              <Button variant="ghost">幽灵键</Button>
              <Button variant="secondary">已选中</Button>
            </div>
          </Demo>

          <Demo index={2} title="弹性开关" description="按住拨钮横向拉长；切换时行进中保持拉长，落位压回原宽。">
            <div className="actions">
              <Switch id="dev-switch" defaultChecked />
              <Label htmlFor="dev-switch">默认尺寸</Label>
              <Switch id="dev-switch-sm" size="sm" />
              <Label htmlFor="dev-switch-sm">小尺寸</Label>
            </div>
          </Demo>

          <Demo index={3} title="水波按钮" description="点在哪，波就从哪散开；键盘回车从中心散开。">
            <Button size="lg" ripple className="w-full">
              大面积主操作
            </Button>
            <div className="grid gap-3 sm:grid-cols-2">
              {["SQLite 单机", "PostgreSQL + Redis"].map((name, i) => (
                <ChoiceCard key={name} selected={choice === i} render={<button type="button" />} onClick={() => setChoice(i)} className="text-left">
                  <strong>{name}</strong>
                  <small className="text-[var(--mg-ink-3)]">点击卡片任意位置</small>
                </ChoiceCard>
              ))}
            </div>
            <OptionRow>
              <Checkbox defaultChecked />
              <span>
                <strong>勾选行</strong>
                <small className="text-[var(--mg-ink-3)]">整行可点，水波从按压点散开</small>
              </span>
            </OptionRow>
          </Demo>

          <Demo index={4} title="点击爆散" description="只留给关键的那一下，例如安装完成。">
            <Button onClick={(e) => burst(e.nativeEvent)}>
              <Sparkles />
              完成关键操作
            </Button>
          </Demo>

          <Demo index={5} title="成功勾选" description="对勾沿路径一笔画出；首屏已勾选的直接显示。">
            <div className="actions">
              <CopyButton variant="outline" value="weir">复制示例文本</CopyButton>
              <Checkbox id="dev-check" checked={checked} onCheckedChange={setChecked} />
              <Label htmlFor="dev-check">复选框</Label>
              <Button variant="outline" onClick={() => toast("已保存", { description: "配置已下发到 3 台 VPS。" })}>成功提示</Button>
            </div>
            <div className="actions">
              <span className="text-[var(--mg-ok)] [&>svg]:size-10">
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
                <CardContent className="overflow-x-auto">
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
                <div className="mg-panel stack p-5">
                  <strong>订阅地址</strong>
                  <small className="text-[var(--mg-ink-3)]">翻到背面查看二维码说明</small>
                  <FlipCardToggle className="justify-self-start">查看背面</FlipCardToggle>
                </div>
              }
              back={
                <div className="mg-panel stack p-5">
                  <strong>二维码</strong>
                  <small className="text-[var(--mg-ink-3)]">用客户端扫码导入，地址只显示一次。</small>
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
              <small className="text-[var(--mg-ink-3)]">移动端导航（宽度 ≤ 900px）使用同一个元件。</small>
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
                  <small className="text-[var(--mg-ink-3)]">向左或向右拖动，甩出即可切到下一张。</small>
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
                  <p className="text-[var(--mg-ink-2)]">匿名访客通过分享链接只能看到订阅组内的节点，不包含用户账号信息。</p>
                </DrawerContent>
              </Drawer>
              <Button variant="outline" onClick={() => setDialog(true)}>打开普通弹窗</Button>
            </div>
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
        </div>
        <FormDemo />
    </div>
  );
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
    <Demo index={14} title="表单校验" description="参照 Element Plus：必填项带红色 *，失焦校验，出错后随输入实时更新；提交时聚焦第一个错误。错误行预留空间，不推动布局。">
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
            <label key={v} className="flex items-center gap-2 text-[13px]"><Radio value={v} />{n}</label>
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
