// 卡片翻面：正反两面叠在同一格里（高度取较高的一面），各自 backface-visibility: hidden 把背面藏好，
// 翻过去不会穿帮。背向用户的一面加 inert，焦点和读屏都进不去。
// 翻面键用 FlipCardToggle 放在任一面里；受控用 flipped / onFlippedChange，非受控用 defaultFlipped。
import { createContext, useContext, useEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { cn } from "cn";
import { Button } from "./button";

const FlipContext = createContext<{ flipped: boolean; flip: () => void } | null>(null);

function FlipCard({
  front,
  back,
  flipped: controlled,
  defaultFlipped = false,
  onFlippedChange,
  className,
}: {
  front: ReactNode;
  back: ReactNode;
  flipped?: boolean;
  defaultFlipped?: boolean;
  onFlippedChange?: (flipped: boolean) => void;
  className?: string;
}) {
  const [inner, setInner] = useState(defaultFlipped);
  const flipped = controlled ?? inner;
  const faces = useRef<[HTMLDivElement | null, HTMLDivElement | null]>([null, null]);
  const moved = useRef(false);
  const flip = () => {
    moved.current = true;
    setInner(!flipped);
    onFlippedChange?.(!flipped);
  };
  // 翻面后原来那面变成 inert，焦点会丢到 body：把焦点交给新一面的翻面键
  useEffect(() => {
    if (!moved.current) return;
    moved.current = false;
    faces.current[flipped ? 1 : 0]?.querySelector<HTMLElement>("[data-slot=flip-toggle]")?.focus({ preventScroll: true });
  }, [flipped]);
  return (
    <FlipContext.Provider value={{ flipped, flip }}>
      <div data-slot="flip-card" className={cn("mg-flip", className)}>
        <div className="mg-flip-inner" data-flipped={flipped || undefined}>
          <div ref={(el) => void (faces.current[0] = el)} className="mg-flip-face" inert={flipped} aria-hidden={flipped || undefined}>
            {front}
          </div>
          <div ref={(el) => void (faces.current[1] = el)} className="mg-flip-face mg-flip-back" inert={!flipped} aria-hidden={!flipped || undefined}>
            {back}
          </div>
        </div>
      </div>
    </FlipContext.Provider>
  );
}

function FlipCardToggle({ children = "翻面", ...props }: Omit<ComponentProps<typeof Button>, "onClick">) {
  const ctx = useContext(FlipContext);
  if (!ctx) throw new Error("FlipCardToggle 需要放在 FlipCard 里");
  return (
    <Button data-slot="flip-toggle" variant="outline" size="sm" aria-pressed={ctx.flipped} onClick={ctx.flip} {...props}>
      {children}
    </Button>
  );
}

export { FlipCard, FlipCardToggle };
