/**
 * 品牌标记：蜂窝六边形（Token 单元）包裹中心枢纽节点，外环留一道缺口暗示「路由中」。
 * 侧栏品牌位、移动端顶栏共用，保证同一枚印记在所有入口一致。
 */
export default function HubMark({ size = 20 }: { size?: number }) {
  return (
    <svg viewBox="0 0 32 32" width={size} height={size} aria-hidden="true">
      {/* 蜂窝外框：顶部留缺口，形成开口而非封闭徽章 */}
      <path
        d="M12.1 4.2 19.9 4.2 27.2 9.6v11.8L19.9 27.8h-7.8L4.8 21.4V9.6Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
        opacity="0.5"
      />
      <path d="M19.9 4.2h4.1" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" opacity="0.5" />
      {/* 中心枢纽：虚线轨道 + 实心节点 */}
      <circle cx="16" cy="16" r="6.4" fill="none" stroke="currentColor" strokeWidth="1.2" strokeDasharray="2.6 3.4" opacity="0.65" />
      <circle cx="16" cy="16" r="2.9" fill="currentColor" />
    </svg>
  )
}