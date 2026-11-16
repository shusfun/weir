// Geometry contracts for shared layouts; this does not judge arbitrary visual composition.
S.checkSpacing = (scope, col) => {
  const px = (value) => parseFloat(value) || 0;
  const round = (value) => Math.round(value * 10) / 10;
  const rootStyle = getComputedStyle(document.documentElement);
  const inset = px(rootStyle.getPropertyValue("--weir-space-3"));
  const gap = px(rootStyle.getPropertyValue("--weir-space-2"));
  const visible = (selector) => [...scope.querySelectorAll(selector)].filter(S.visible);
  for (const header of visible('.stack > [data-slot="section-header"]')) {
    const parent = getComputedStyle(header.parentElement), style = getComputedStyle(header);
    if (px(parent.rowGap) > 0 && px(style.marginBottom) > 0)
      col.add("spacing", header, "标题间距叠加", `gap ${parent.rowGap} + margin-bottom ${style.marginBottom}`, "由 stack 的 gap 统一控制间距");
  }
  for (const section of visible('[data-slot="collapsible"][data-variant="section"]')) {
    const trigger = section.querySelector('[data-slot="collapsible-trigger"]');
    const style = getComputedStyle(trigger);
    if (Math.min(px(style.paddingLeft), px(style.paddingRight)) < inset - 1)
      col.add("spacing", trigger, "折叠标题贴边", style.padding, `横向 padding ≥ ${inset}px`);
    const body = section.querySelector('.weir-collapsible-section-body');
    if (trigger.getAttribute('aria-expanded') !== 'true' || !body || !S.visible(body)) continue;
    const title = trigger.querySelector('[data-slot="collapsible-title"]');
    const content = [...body.children].filter(S.visible).filter(el => getComputedStyle(el).position !== 'fixed');
    if (title && content.length) {
      const delta = content[0].getBoundingClientRect().left - title.getBoundingClientRect().left;
      if (Math.abs(delta) > 1) col.add("spacing", body, "折叠正文缩进", `${round(delta)}px`, "正文与标题左边缘一致");
    }
    for (let i = 1; i < content.length; i++) {
      const distance = content[i].getBoundingClientRect().top - content[i - 1].getBoundingClientRect().bottom;
      if (distance < gap - 1) col.add("spacing", content[i], "折叠内容拥挤", `${round(distance)}px`, `相邻内容间距 ≥ ${gap}px`);
    }
  }
  for (const row of visible('[data-tour="gate-management"] .weir-node-management-row')) {
    const style = getComputedStyle(row);
    if (px(style.paddingLeft) > 1 || px(style.paddingRight) > 1)
      col.add("spacing", row, "节点行重复缩进", `${style.paddingLeft} / ${style.paddingRight}`, "沿用外层容器的横向留白");
  }
  for (const dialog of visible('[data-slot="dialog-content"]')) {
    const title = dialog.querySelector('[data-slot="dialog-title"]');
    const body = dialog.querySelector('[data-slot="dialog-body"] [data-slot="scroll-area-content"]');
    if (!title || !body) continue;
    const delta = body.getBoundingClientRect().left - title.getBoundingClientRect().left;
    if (Math.abs(delta) > 1) col.add("spacing", body, "弹窗正文缩进", `${round(delta)}px`, "正文轨道与标题左边缘一致");
  }
  for (const group of visible('[data-slot="action-group"][data-wrap="false"]')) {
    const buttons = [...group.children].filter(el => el.matches('.weir-button') && S.visible(el));
    const tops = buttons.map(el => el.getBoundingClientRect().top);
    if (tops.length > 1 && Math.max(...tops) - Math.min(...tops) > 1)
      col.add("spacing", group, "操作组拆行", tops.map(round).join(" / "), "整组按钮保持同排");
  }
};
