// 拖拽排序列表（@dnd-kit）：行首把手可用鼠标拖、触屏长按后拖（不抢页面滚动）、键盘拿起 / 移动 / 放下，
// 读屏播报中文；拖动中的行用 transform 抬起，其他行让位有动画，放下回弹。减少动态效果时不做动画。
// 不能拖的场景仍有上移 / 下移按钮：renderItem 拿到的 reorder 由调用方放在行内合适的位置。
//
// 行的标识：传 getId 时用它（数据自带稳定 id）；不传时按位置分配，并在本组件内的排序时跟着移动，
// 这样行内输入框在拖动后不会因为重新挂载丢失焦点。增删行由调用方改 items，按位置补齐或截断。
import { useRef, useState, type CSSProperties, type ReactNode } from "react";
import {
  DndContext,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  closestCenter,
  useSensor,
  useSensors,
  type Announcements,
  type DragEndEvent,
  type KeyboardSensorOptions,
  type Modifier,
  type Sensor,
  type UniqueIdentifier,
} from "@dnd-kit/core";
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical } from "lucide-react";
import { cn } from "cn";
import { Button } from "./button";
import { ReorderButtons } from "./reorder-buttons";
import { usePrefersReducedMotion } from "./motion";

type SortableRenderContext = {
  index: number;
  // 上移 / 下移按钮（ReorderButtons），首尾自动禁用
  reorder: ReactNode;
  dragging: boolean;
};

type SortableListProps<T> = {
  items: T[];
  // 新顺序，以及被移动的行原来和现在的位置
  onReorder: (next: T[], from: number, to: number) => void;
  renderItem: (item: T, ctx: SortableRenderContext) => ReactNode;
  // 读屏与按钮文案里的行名，如「步骤 1」
  itemLabel: (item: T, index: number) => string;
  getId?: (item: T, index: number) => UniqueIdentifier;
  // 整个列表的名称（读屏）
  label?: string;
  disabled?: boolean;
  className?: string;
  itemClassName?: string;
};

// dnd-kit 的键盘传感器在 document 冒泡阶段收按键；列表放在弹窗、抽屉等 Base UI 门户里时，
// 门户会拦下按键冒泡，方向键收不到，Esc 还会先关掉整个弹窗。改为在 window 捕获阶段最先收到，
// 拖动中由本传感器处理的键不再往下传，Esc 只取消拖动、不关弹窗。
const dragKeys = new Set(["Space", "Enter", "Escape", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"]);
// attach 在类型声明里是私有的，按普通构造函数继承
const KeyboardSensorBase = KeyboardSensor as unknown as new (props: ConstructorParameters<typeof KeyboardSensor>[0]) => object;
class CaptureKeyboardSensorImpl extends KeyboardSensorBase {
  static activators = KeyboardSensor.activators;
  attach() {
    const self = this as unknown as {
      handleStart: () => void;
      handleKeyDown: (e: KeyboardEvent) => void;
      handleCancel: (e: Event) => void;
      windowListeners: { add: (name: string, fn: (e: Event) => void, options?: AddEventListenerOptions) => void };
    };
    self.handleStart();
    self.windowListeners.add("resize", self.handleCancel);
    self.windowListeners.add("visibilitychange", self.handleCancel);
    // 和原实现一样晚一拍挂上：拿起用的那次空格 / 回车不被当成放下
    setTimeout(() =>
      self.windowListeners.add(
        "keydown",
        (e) => {
          const key = e as KeyboardEvent;
          if (!dragKeys.has(key.code)) return;
          key.stopPropagation();
          self.handleKeyDown(key);
        },
        { capture: true },
      ),
    );
  }
}
const CaptureKeyboardSensor = CaptureKeyboardSensorImpl as unknown as Sensor<KeyboardSensorOptions>;

// 移动时间与曲线对应 --weir-mid / --weir-spring（dnd-kit 要数字毫秒，曲线可直接写 CSS 变量）
const moveTransition = { duration: 220, easing: "var(--weir-spring)" };
// 只沿纵向移动
const lockX: Modifier = ({ transform }) => ({ ...transform, x: 0 });

function usePositionIds(count: number) {
  const ids = useRef<string[]>([]);
  const seq = useRef(0);
  if (ids.current.length > count) ids.current = ids.current.slice(0, count);
  while (ids.current.length < count) ids.current.push("row-" + seq.current++);
  return ids;
}

function SortableList<T>({ items, onReorder, renderItem, itemLabel, getId, label, disabled = false, className, itemClassName }: SortableListProps<T>) {
  const reduced = usePrefersReducedMotion();
  const positionIds = usePositionIds(items.length);
  const ids = getId ? items.map((item, i) => getId(item, i)) : positionIds.current;
  const [activeId, setActiveId] = useState<UniqueIdentifier | null>(null);
  const justPicked = useRef(false);
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 4 } }),
    // 长按 250 ms 才拿起：期间手指移动超过 8px 视为滚动页面，不拿起
    useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 8 } }),
    // 目标行在滚动区外时传感器先滚动容器：减少动态效果时直接跳到位，不平滑滚动
    useSensor(CaptureKeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates, scrollBehavior: reduced ? "auto" : "smooth" }),
  );

  const move = (from: number, to: number) => {
    if (from === to || to < 0 || to >= items.length) return;
    if (!getId) positionIds.current = arrayMove(positionIds.current, from, to);
    onReorder(arrayMove(items, from, to), from, to);
  };
  const nameOf = (id: UniqueIdentifier) => {
    const i = ids.indexOf(id);
    return i < 0 ? "该项" : itemLabel(items[i], i);
  };
  const positionOf = (id: UniqueIdentifier | undefined) => (id === undefined ? -1 : ids.indexOf(id) + 1);
  const announcements: Announcements = {
    onDragStart: ({ active }) => `已拿起 ${nameOf(active.id)}，当前第 ${positionOf(active.id)} 位，共 ${items.length} 项。`,
    // 拿起后 dnd-kit 立刻报一次「在自己上方」，不播报，免得盖掉「已拿起」
    onDragOver: ({ active, over }) => (!over ? `${nameOf(active.id)} 不在列表范围内。` : over.id === active.id && justPicked.current ? ((justPicked.current = false), undefined) : `${nameOf(active.id)} 移到第 ${positionOf(over.id)} 位。`),
    onDragEnd: ({ active, over }) => (over ? `已放下 ${nameOf(active.id)}，现在是第 ${positionOf(over.id)} 位。` : `已放下 ${nameOf(active.id)}，顺序未变。`),
    onDragCancel: ({ active }) => `已取消拖动，${nameOf(active.id)} 回到原位。`,
  };

  function onDragEnd({ active, over }: DragEndEvent) {
    setActiveId(null);
    if (!over || active.id === over.id) return;
    move(ids.indexOf(active.id), ids.indexOf(over.id));
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      modifiers={[lockX]}
      onDragStart={({ active }) => {
        justPicked.current = true;
        setActiveId(active.id);
      }}
      onDragEnd={onDragEnd}
      onDragCancel={() => setActiveId(null)}
      accessibility={{
        announcements,
        screenReaderInstructions: { draggable: "按空格键或回车键拿起，用上下方向键移动，再按空格键或回车键放下，按 Esc 取消。" },
      }}
    >
      <SortableContext items={ids} strategy={verticalListSortingStrategy} disabled={disabled}>
        <div data-slot="sortable-list" role="list" aria-label={label} className={cn("weir-sortable-list", className)}>
          {items.map((item, i) => (
            <SortableRow key={ids[i]} id={ids[i]} name={itemLabel(item, i)} disabled={disabled} reduced={reduced} className={itemClassName}>
              {renderItem(item, {
                index: i,
                dragging: activeId === ids[i],
                reorder: (
                  <ReorderButtons
                    label={itemLabel(item, i)}
                    onUp={() => move(i, i - 1)}
                    onDown={() => move(i, i + 1)}
                    upDisabled={disabled || i === 0}
                    downDisabled={disabled || i === items.length - 1}
                  />
                ),
              })}
            </SortableRow>
          ))}
        </div>
      </SortableContext>
    </DndContext>
  );
}

function SortableRow({
  id,
  name,
  disabled,
  reduced,
  className,
  children,
}: {
  id: UniqueIdentifier;
  name: string;
  disabled: boolean;
  reduced: boolean;
  className?: string;
  children: ReactNode;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id,
    disabled,
    transition: reduced ? null : moveTransition,
  });
  const style: CSSProperties = {
    transform: CSS.Translate.toString(transform),
    transition: reduced ? undefined : transition,
  };
  return (
    <div ref={setNodeRef} role="listitem" data-slot="sortable-item" data-dragging={isDragging || undefined} className={cn("weir-sortable-item", className)} style={style}>
      <Button
        ref={setActivatorNodeRef}
        type="button"
        variant="ghost"
        size="icon-sm"
        className="weir-sortable-handle"
        aria-label={"拖动排序 " + name}
        disabled={disabled}
        {...attributes}
        // 把手本身是按钮，不需要 dnd-kit 补的 role="button"
        role={undefined}
        {...listeners}
      >
        <GripVertical />
      </Button>
      <div className="weir-sortable-body">{children}</div>
    </div>
  );
}

export { SortableList };
export type { SortableListProps, SortableRenderContext };
