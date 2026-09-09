import { useCallback, useEffect, useState } from 'react'
import { Button, Card, Input, Table, Tag, Typography, message, Popconfirm } from 'antd'
import { PlusOutlined, DeleteOutlined, ReloadOutlined, InfoCircleOutlined } from '@ant-design/icons'
import { request } from '../api/http'
import { Reveal } from '../utils/motion'

const { Text } = Typography

/** 会话标识配置行（后端 model.SessionHeaderConfig）。 */
interface SessionHeaderConfig {
  id: number
  key: string
  header_name: string
  description: string
  enabled: boolean
  created_at: string
  updated_at: string
}

/** 会话标识配置页面：配置可识别的 header 中的 session 数据 key。 */
export default function Settings() {
  const [configs, setConfigs] = useState<SessionHeaderConfig[]>([])
  const [newHeader, setNewHeader] = useState('')
  const [newDescription, setNewDescription] = useState('')
  const [loading, setLoading] = useState(false)
  const [adding, setAdding] = useState(false)

  // 加载配置：后端信封 {code, message, data} 已由 request() 解包
  const loadConfigs = useCallback(async () => {
    setLoading(true)
    try {
      const data = await request<{ configs: SessionHeaderConfig[] }>({
        url: '/api/v1/settings/session-headers',
        method: 'GET',
      })
      setConfigs(data.configs ?? [])
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载配置失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadConfigs()
  }, [loadConfigs])

  // 添加新配置：即时落库，成功后重拉列表
  const addConfig = useCallback(async () => {
    const name = newHeader.trim()
    if (!name) {
      message.warning('请输入 Header 名称')
      return
    }
    const exists = configs.some((c) => c.header_name.toLowerCase() === name.toLowerCase())
    if (exists) {
      message.warning('该 Header 已存在')
      return
    }
    setAdding(true)
    try {
      await request<SessionHeaderConfig>({
        url: '/api/v1/settings/session-headers',
        method: 'POST',
        data: { header_name: name, description: newDescription.trim() },
      })
      message.success('添加成功')
      setNewHeader('')
      setNewDescription('')
      await loadConfigs()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '添加失败')
    } finally {
      setAdding(false)
    }
  }, [newHeader, newDescription, configs, loadConfigs])

  // 删除配置
  const removeConfig = useCallback(
    async (id: number) => {
      try {
        await request({ url: `/api/v1/settings/session-headers/${id}`, method: 'DELETE' })
        message.success('删除成功')
        await loadConfigs()
      } catch (error) {
        message.error(error instanceof Error ? error.message : '删除失败')
      }
    },
    [loadConfigs],
  )

  // 切换启用状态（按配置 key）
  const toggleEnabled = useCallback(
    async (key: string) => {
      try {
        await request({ url: `/api/v1/settings/session-headers/${key}/toggle`, method: 'PUT' })
        await loadConfigs()
      } catch (error) {
        message.error(error instanceof Error ? error.message : '更新失败')
      }
    },
    [loadConfigs],
  )

  const columns = [
    {
      title: 'Header 名称',
      dataIndex: 'header_name',
      key: 'header_name',
      render: (text: string) => (
        <Text code style={{ fontSize: 13 }}>
          {text}
        </Text>
      ),
    },
    {
      title: '描述',
      dataIndex: 'description',
      key: 'description',
      render: (text: string) => (
        <Text type="secondary" style={{ fontSize: 13 }}>
          {text || '—'}
        </Text>
      ),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 100,
      render: (enabled: boolean, record: SessionHeaderConfig) => (
        <Tag
          color={enabled ? 'success' : 'default'}
          style={{ cursor: 'pointer' }}
          onClick={() => void toggleEnabled(record.key)}
        >
          {enabled ? '启用' : '禁用'}
        </Tag>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 80,
      render: (_: unknown, record: SessionHeaderConfig) => (
        <Popconfirm
          title="确定删除此配置？"
          description="删除后将不再识别该 Header 中的会话标识"
          onConfirm={() => void removeConfig(record.id)}
          okText="删除"
          cancelText="取消"
        >
          <Button type="text" danger icon={<DeleteOutlined />} size="small" />
        </Popconfirm>
      ),
    },
  ]

  return (
    <Reveal style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <InfoCircleOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>会话标识配置</span>
          </div>
        }
      >
        <div style={{ marginBottom: 16 }}>
          <Text type="secondary" style={{ fontSize: 13 }}>
            配置可识别的 HTTP Header 中的会话标识。网关会按这里启用的 Header
            依次读取（取第一个非空值）写入请求日志的 session_id，仪表盘据此对同一会话的请求进行聚合。
          </Text>
        </div>

        <Table
          dataSource={configs}
          columns={columns}
          rowKey="id"
          pagination={false}
          size="small"
          loading={loading}
          style={{ marginBottom: 16 }}
        />

        <div
          style={{
            display: 'flex',
            gap: 12,
            alignItems: 'flex-end',
            padding: '12px 0 0',
            borderTop: '1px solid var(--border-faint)',
          }}
        >
          <div style={{ flex: 1 }}>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
              Header 名称
            </Text>
            <Input
              placeholder="例如: X-Conversation-Id"
              value={newHeader}
              onChange={(e) => setNewHeader(e.target.value)}
              onPressEnter={() => void addConfig()}
              style={{ width: '100%' }}
            />
          </div>
          <div style={{ flex: 1 }}>
            <Text type="secondary" style={{ fontSize: 12, marginBottom: 4, display: 'block' }}>
              描述（可选）
            </Text>
            <Input
              placeholder="例如: 自定义会话标识"
              value={newDescription}
              onChange={(e) => setNewDescription(e.target.value)}
              onPressEnter={() => void addConfig()}
              style={{ width: '100%' }}
            />
          </div>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => void addConfig()} loading={adding}>
            添加
          </Button>
        </div>
      </Card>

      <Card
        className="panel"
        styles={{
          body: { padding: '16px 20px' },
          header: { borderBottom: '1px solid var(--border-faint)' },
        }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <InfoCircleOutlined style={{ color: 'var(--accent)' }} />
            <span style={{ fontSize: 14, fontWeight: 500 }}>使用说明</span>
          </div>
        }
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div>
            <Text strong style={{ fontSize: 13 }}>会话聚合规则</Text>
            <ul style={{ margin: '8px 0 0 20px', padding: 0 }}>
              <li>
                <Text type="secondary" style={{ fontSize: 13 }}>
                  网关按启用的 Header 依次读取，取第一个非空值作为会话标识
                </Text>
              </li>
              <li>
                <Text type="secondary" style={{ fontSize: 13 }}>
                  同一会话的所有请求会在仪表盘中合并显示
                </Text>
              </li>
              <li>
                <Text type="secondary" style={{ fontSize: 13 }}>
                  配置即时生效（含短缓存），无需重启服务
                </Text>
              </li>
            </ul>
          </div>
          <div>
            <Text strong style={{ fontSize: 13 }}>示例</Text>
            <div
              style={{
                background: 'var(--bg-elevated)',
                borderRadius: 6,
                padding: '12px 16px',
                marginTop: 8,
                fontFamily: 'monospace',
                fontSize: 13,
              }}
            >
              <div style={{ color: 'var(--text-faint)' }}># 请求示例</div>
              <div>POST /v1/chat/completions</div>
              <div style={{ color: 'var(--accent)' }}>X-Session-Id: sess_abc123</div>
              <div style={{ color: 'var(--accent)' }}>X-Conversation-Id: conv_xyz789</div>
            </div>
          </div>
        </div>
      </Card>

      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 12 }}>
        <Button icon={<ReloadOutlined />} onClick={() => void loadConfigs()} loading={loading}>
          重新加载
        </Button>
      </div>
    </Reveal>
  )
}
