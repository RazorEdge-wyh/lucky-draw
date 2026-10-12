# 更新日志

本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [1.0.0] - 2026-10-12

首个可用版本。

### 新增

- **核心算法**（`internal/engine`）
  - 加权号码池：累积概率表 + 线性扫描，特殊号码权重可配置。
  - 整轮约束：特殊号码必须是本轮唯一最高票，否则整轮作废重抽。
  - 重抽上限 `MAX_ATTEMPTS` + 构造式兜底，保证接口永不挂起；触发兜底时响应标记 `fallback` 并打 warn 日志。
  - 解析命中率 `AcceptProb`：整数 DP 精确计算多项式系数之和，无浮点误差。
- **随机源**（`internal/randsource`）
  - 生产实现直接调用 `crypto/rand`，不做「播种一次 math/rand」——避免长期运行后状态被反推。
  - `Intn` 用拒绝采样消除取模偏置，`Shuffle` 用 Fisher–Yates。
  - 可复现的确定性实现 `Seeded`，供测试与审计使用。
- **HTTP 层**（`internal/httpapi`）
  - `GET /api/draw`：抽奖，支持 `count`/`scenario`/`drawCount`/`maxNumber`/`specialProb` 参数。
  - `GET /api/meta`：生效配置、模式列表、权重表、命中率与期望重抽轮数。
  - `GET /api/simulate`：蒙特卡洛自检，实测分布与理论值对照。
  - `GET /healthz`：存活探针。
  - 参数变体引擎缓存（上限 64 条，防内存膨胀）、panic 恢复、访问日志、安全响应头。
  - 内置单页控制台（`go:embed`，零外部依赖，离线可用）。
- **玩法模式**（`internal/scenario`）：`challenging`（默认）/ `fair`（真等概率）/ `hot`（加权无约束）。
- **命令行工具**（`cmd/draw`）：`-n` / `-json` / `-config` / `-simulate` / `-bench`。
- **工程化**：Dockerfile（多阶段 + distroless）、docker compose、Makefile、
  GitHub Actions CI（gofmt + vet + 竞态测试 + 覆盖率 + 冒烟测试 + 镜像构建）、OpenAPI 3.0 规范。

### 测试

- 核心不变量：20000 轮随机结果中，特殊号码必须出现且严格多于其他每个号码。
- 概率公式三路交叉验证：穷举全部票数向量、逐 k 蒙特卡洛采样（40 万轮）、组合恒等式。
- 加权抽样分布校验：40 万次抽样中各号码频率与权重偏差 < 0.004。
- 兜底路径、无解参数、并发安全、确定性复现、HTTP 参数校验与错误码。

### 已知限制

- 默认模式**不是**均匀等概率抽奖，结果中特殊号码占比被条件化抬高（约 37%）。详见 README 开头的诚实说明。
- 单机内存态，无分布式一致性：多副本部署时各副本的随机序列互不相关（对抽奖场景无影响）。
- 未内置限流与鉴权。面向公网部署时请在网关层补充。
