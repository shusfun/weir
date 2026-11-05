// 产品 logo：动图；系统开启「减少动态效果」时换成第一帧。大小与圆角由外层容器决定，名称由外层文字或链接提供。
export function BrandMark() {
  return (
    <picture className="contents">
      <source media="(prefers-reduced-motion: reduce)" srcSet="/avatar-still.png" />
      <img src="/avatar.gif" alt="" className="block size-full object-cover" />
    </picture>
  );
}
