import { request } from './http'

/** 清理触发模式。 */
export type CleanupMode = 'daily' | 'interval'

/** 数据管理设置（后端 model.DataSettings 单行，配置 + 后台任务写回的运行状态）。 */
export interface DataSettings {
  id: number
  cleanup_enabled: boolean
  cleanup_mode: CleanupMode
  /** daily 模式每日执行时刻（0-23，本地时区）。 */
  cleanup_run_hour: number
  /** interval 模式执行间隔（小时）。 */
  cleanup_interval_hours: number
  /** 保留最近 N 天日志（累计统计读聚合表，不受影响）。 */
  cleanup_max_days: number
  cleanup_batch_size: number
  cleanup_vacuum: boolean
  /** 请求头瘦身开关。 */
  trim_headers: boolean
  /** 请求头保留天数（置空更早的行）。 */
  trim_headers_after_days: number
  // 聚合任务写回
  agg_watermark_day: number
  agg_last_run_at: string
  // 清理任务写回
  last_cleanup_at: string
  last_cleanup_day: number
  last_deleted_rows: number
  last_trimmed_rows: number
  last_duration_ms: number
  last_error: string
  updated_at: string
}

/** 清理配置更新入参（后端 service.UpdateDataSettingsInput）。 */
export interface UpdateDataSettingsInput {
  enabled: boolean
  mode: CleanupMode
  run_hour: number
  interval_hours: number
  max_days: number
  batch_size: number
  vacuum: boolean
  trim_headers: boolean
  trim_headers_after_days: number
}

/** 日级聚合状态（后端 aggregation.Status）。 */
export interface AggregateStatus {
  watermark_day: number
  watermark_date: string
  last_run_at: string
  earliest_day: string
  latest_day: string
}

/** GET /api/v1/settings/data 响应。 */
export interface DataSettingsView {
  cleanup: DataSettings
  aggregate: AggregateStatus
}

export const dataSettingsApi = {
  /** 读取清理配置/状态与聚合状态。 */
  get: () => request<DataSettingsView>({ url: '/api/v1/settings/data', method: 'GET' }),
  /** 更新清理配置（保存后后台任务下一轮生效）。 */
  update: (payload: UpdateDataSettingsInput) =>
    request<DataSettings>({ url: '/api/v1/settings/data', method: 'PUT', data: payload }),
  /** 重建聚合：days > 0 重建近 N 天，0 = 全量（以现存明细为准）。 */
  rebuildAggregate: (days = 0) =>
    request<AggregateStatus>({
      url: '/api/v1/settings/data/aggregate/rebuild',
      method: 'POST',
      data: { days },
    }),
}
