# 贡献指南

感谢关注 XTokenHub！欢迎以任何形式贡献：Bug 报告、功能建议、文档改进、代码 PR。

## 开发环境

```bash
# 后端（Go >= 1.22，零 CGO）
make test          # go test ./... -race 必须全绿

# 前端（pnpm）
cd frontend && pnpm install
pnpm test:run      # Vitest 必须全绿

# 一体构建验证
make build && ./bin/xtokenhub -config configs/config.yaml

# macOS 菜单栏应用（需要 Xcode + xcodegen）
cd XTokenHubMenuBar && make test
```

## 提交 PR 前

- 新功能请配单元测试（测试放在 `internal/tests/<模块>/`，外部测试包风格）
- 保持核心原则：**原生协议优先透传，转换只作兜底**；新增转换路径需说明覆盖范围
- 提交信息用中文或英文均可，格式参考现有提交（`feat: ...` / `fix: ...` / `docs: ...`）
- 涉及安全的设计（密钥存储、鉴权）请先开 issue 讨论

## 报告安全漏洞

请勿公开发布安全 issue，通过 [Security Advisories](https://github.com/xgd16/XTokenHub/security/advisories/new) 私密报告。
