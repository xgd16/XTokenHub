/** 回退格子尺寸：容器尚未测量到（首帧/测试环境）时使用。 */
export const HEATMAP_FALLBACK_CELL = 10
/** 自适应下限：低于该尺寸改为横向滚动，保证可点。 */
export const HEATMAP_MIN_CELL = 8
/** 自适应上限：避免超宽屏下格子大得突兀。 */
export const HEATMAP_MAX_CELL = 40
/** 间隙随格子等比缩放。 */
export const HEATMAP_GAP_RATIO = 0.18
export const HEATMAP_PAD = { l: 6, r: 6, b: 4 }
/** 月份标签与格子的垂直间隙。 */
export const HEATMAP_LABEL_GAP = 8
/** 容器最小高度：避免首帧塌陷，同时保住窄屏可用性。 */
export const HEATMAP_MIN_BOX_H = 120
/** 卡片偏扁时，热力图最多把容器抬到该高度去撑满宽度（同行卡片会被一并拉高，故设上限）。 */
export const HEATMAP_MAX_BOX_H = 300

export interface HeatmapLayout {
  cell: number
  gap: number
  labelFs: number
  padT: number
  /** 整个 SVG/画布所需的像素宽度。 */
  W: number
  /** 整个 SVG/画布所需的像素高度。 */
  H: number
  /** 撑满宽度所需的容器高度。 */
  boxMinH: number
}

/** 给定格子尺寸下的完整排版结果：宽高都由真实 gap / 标签高度推导，供反解与收缩共用。 */
function layout(cell: number, cols: number, rows: number) {
  const gap = Math.max(2, Math.round(cell * HEATMAP_GAP_RATIO))
  const labelFs = Math.max(10, Math.min(18, Math.round(cell * 0.6)))
  const padT = labelFs + HEATMAP_LABEL_GAP
  return {
    cell,
    gap,
    labelFs,
    padT,
    W: HEATMAP_PAD.l + cols * (cell + gap) - gap + HEATMAP_PAD.r,
    H: padT + rows * (cell + gap) - gap + HEATMAP_PAD.b,
  }
}

/**
 * 由容器可用宽高反解格子尺寸：
 * - 宽度优先撑满，超过 MAX_CELL 不再放大；
 * - 窄屏保底 MIN_CELL，允许父容器横向滚动；
 * - 高度是硬约束，放不下继续收缩；
 * - 容器被同行卡片拉高时最多把 boxMinH 抬到 MAX_BOX_H。
 */
export function computeHeatmapLayout(
  cols: number,
  rows: number,
  boxW: number | null,
  boxH: number | null,
): HeatmapLayout {
  if (cols <= 0) {
    const fallback = layout(HEATMAP_FALLBACK_CELL, 1, Math.max(rows, 1))
    return { ...fallback, boxMinH: HEATMAP_MIN_BOX_H }
  }
  const safeRows = Math.max(rows, 1)

  const availW = boxW != null && boxW > 0 ? boxW - HEATMAP_PAD.l - HEATMAP_PAD.r : null
  let cell =
    availW == null
      ? HEATMAP_FALLBACK_CELL
      : Math.min(HEATMAP_MAX_CELL, Math.floor(availW / (cols + (cols - 1) * HEATMAP_GAP_RATIO)))
  if (cell < HEATMAP_MIN_CELL) cell = HEATMAP_MIN_CELL // 容器过窄：保底尺寸 + 横向滚动

  // 反解用了连续近似、gap 又要取整，横向再收敛一次（MIN_CELL 时保留横向滚动）
  while (cell > HEATMAP_MIN_CELL && availW != null && layout(cell, cols, safeRows).W > availW) cell--

  // 撑满宽度所需的高度：容器被同行卡片拉得比这还高时无副作用，比这矮就把容器抬起来
  const boxMinH =
    availW == null
      ? HEATMAP_MIN_BOX_H
      : Math.min(
          HEATMAP_MAX_BOX_H,
          Math.max(HEATMAP_MIN_BOX_H, layout(cell, cols, safeRows).H),
        )
  const availH = Math.max(boxMinH, boxH && boxH > 0 ? boxH : 0)

  // 高度是硬约束：正方格子在可用高度内放不下就继续收缩
  while (cell > HEATMAP_MIN_CELL && layout(cell, cols, safeRows).H > availH) cell--
  return { ...layout(cell, cols, safeRows), boxMinH }
}
