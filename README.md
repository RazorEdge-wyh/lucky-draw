<div align="center">

# 🎲 lucky-draw

**游戏奖品抽取用的「随机」数生成器**

加权随机抽样 · 密码学安全随机源 · 整轮约束校验 · 内置网页控制台 · 单二进制部署

[![CI](https://github.com/RazorEdge-wyh/lucky-draw/actions/workflows/ci.yml/badge.svg)](https://github.com/RazorEdge-wyh/lucky-draw/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

---

## ⚠️ 先看这里：关于「随机」的诚实说明

这个项目的名字里有「随机」，但它**不是均匀等概率抽奖**。在动手用之前，请务必理解它在做什么：

| 问题 | 答案 |
| --- | --- |
| 每个号码被抽中的概率一样吗？ | **不一样。** 特殊号码（默认 4 号）单次权重 15%，其余 7 个号码各约 12.14%。 |
| 每轮结果里特殊号码一定会赢吗？ | **一定。** 若特殊号码不是本轮唯一最高票，**整轮作废重抽**，直到满足为止。 |
| 玩家能预测下一轮结果吗？ | **不能。** 随机源来自 `crypto/rand`，没有可被反推的内部状态。 |
| 那「随机」体现在哪？ | 体现在**其余号码的分布**与票型细节上，而不是特殊号码是否获胜。 |

换句话说：**特殊号码是否中奖由约束决定，其余号码如何分布由随机决定。**

如果你的场景需要真正等概率、无偏袒的抽奖（例如公开摇号、合规审计场景），
请使用内置的 `scenario=fair` 模式，或直接换用等概率实现。
**不要**用本项目的默认模式对外宣称「完全公平的等概率抽奖」——那是虚假宣传，也可能违反你所在地区的抽奖法规。

算法细节、概率推导与实测数据见下文 [算法说明](#算法说明)。

---

## ✨ 功能特性

- **加权抽样**：每个号码的概率权重可配置，累积概率表 + 线性扫描（号码池小，比二分更快）。
- **整轮约束**：保证特殊号码成为唯一最高票，不满足则整轮重抽；带重抽上限与兜底构造，**接口永不挂起**。
- **密码学安全随机源**：直接使用 `crypto/rand`，而非「播种一次 math/rand」——后者长期运行后状态可被反推。
- **解析概率自检**：`/api/meta` 与 `/api/simulate` 会给出单轮自然命中率与期望重抽轮数，并与实测对照。
- **内置网页控制台**：零依赖、零 CDN 的单页界面，编译进二进制，离线可用。
- **三种玩法模式**：挑战模式（默认，带约束）/ 等概率模式（真公平）/ 加权模式（不保证票型）。
- **可复现**：引擎接受注入随机源，相同种子产生完全相同结果，便于审计对账与回归测试。
- **工程质量**：`go vet` 干净、`gofmt` 干净、单测覆盖核心不变量与概率公式、CI 含竞态检测与端到端冒烟测试。

---

## 🚀 快速开始

### 方式一：本地运行（需要 Go 1.22+）

```bash
git clone https://github.com/RazorEdge-wyh/lucky-draw.git
cd lucky-draw
go run .
```

打开 <http://localhost:8080> 即可看到控制台。

### 方式二：Docker

```bash
docker build -t lucky-draw .
docker run --rm -p 8080:8080 lucky-draw
```

### 方式三：docker compose

```bash
docker compose up --build -d
```

### 方式四：命令行抽奖（不起服务）

```bash
go run ./cmd/draw                 # 抽一轮
go run ./cmd/draw -n 5            # 连抽 5 轮
go run ./cmd/draw -json           # JSON 输出，便于管道处理
go run ./cmd/draw -config         # 查看生效配置与权重表
go run ./cmd/draw -simulate 100000  # 蒙特卡洛自检
go run ./cmd/draw -bench          # 压测吞吐
```

### 最快验证：一行 curl

```bash
curl -s http://localhost:8080/api/draw | python3 -m json.tool
```

### 二进制自带的开关

```bash
./lucky-draw -version        # 打印版本号
./lucky-draw -check          # 只做配置自检，不启动服务（适合 CI / 部署前校验）
./lucky-draw -healthcheck    # 请求本机 /healthz，成功退出 0，失败退出 1
```

`-healthcheck` 是给容器用的：最终镜像是 distroless，里面没有 `curl`/`wget`，
所以健康探测由二进制自己发起 HTTP 请求，`HEALTHCHECK` 直接调用它。

---

## 🧠 算法说明

### 目标

需求可以精确表述为：**从号码池抽 N 次，使得特殊号码出现的次数严格多于其他任何号码。**
默认参数下 N=9、号码池 1..8、特殊号码 4、特殊号码单次权重 15%。

### 两个阶段

```
                    ┌─────────────────────────────────────┐
                    │  阶段一：加权抽样（每轮 N 次）        │
                    │  特殊号码 15% ┊ 其余 7 个号码各 12.14% │
                    └──────────────┬──────────────────────┘
                                   │
                                   ▼
                    ┌─────────────────────────────────────┐
                    │  阶段二：整轮校验                    │
                    │  特殊号码出现次数 > 其他每一个号码？  │
                    └──────┬───────────────────┬──────────┘
                       是  │                   │  否
                           ▼                   ▼
                      返回结果          整轮作废，重抽
                                        （最多 MAX_ATTEMPTS 次，
                                          超限则启用兜底构造）
```

**关键点：重抽是对「整轮」重来，而不是只换掉冲突的那几个号码。** 这保证了结果的联合分布是可推导的，也让内部重抽次数成为一个可观测、可告警的指标。

### 概率推导

一轮结果服从参数为 $(p, q)$ 的多项式分布，其中 $p = 0.15$ 是特殊号码权重，
$q = (1-p)/m$（$m=7$）是每个其他号码的权重。

设特殊号码出现 $k$ 次。「特殊号码唯一最多」等价于：其他每个号码出现次数都 $\le k-1$。
于是单轮自然命中率为

$$
P(\text{命中}) = \sum_{k=1}^{n} \binom{n}{k} \cdot M(n-k,\ m,\ k-1) \cdot p^{k} \cdot q^{\,n-k}
$$

其中 $M(r, m, L) = \sum \dfrac{r!}{\prod_i c_i!}$ 是对所有满足
$\sum_i c_i = r$、$0 \le c_i \le L$ 的计数向量求和（**多项式系数之和**，不是解的个数——
每个计数向量对应 $\frac{r!}{\prod c_i!}$ 条不同的抽取序列，漏掉这个因子会把概率算小数百倍）。

$M$ 用整数动态规划精确计算：

```
dp[0] = 1
for 每个盒子:
    for j in 0..r:
        for c in 0..L:
            dp[j+c] += dp[j] * C(r-j, c)
```

### 默认参数下的实际数字

| 项目 | 数值 |
| --- | --- |
| 特殊号码单次权重 | 15% |
| 其他号码单次权重 | 12.1428…%（各） |
| **单轮自然命中率** | **0.1113053** |
| **期望重抽轮数** | **8.98 轮** |
| 实测平均重抽轮数（40 万轮采样） | 8.98 轮 |
| 单轮 `Draw()` 耗时（含平均重抽 9 次） | 约 8–10 µs / 2.5 KB / 27 次分配 |
| 单次加权抽样 `pick()` | 约 79 ns / 0 次分配（不含 `crypto/rand` 读数） |

> 上面的耗时来自 AMD Ryzen 7 5800X 上的 `go test -bench`。
> 注意 `pick()` 的基准用的是确定性随机源；生产路径每个随机数都要读一次
> `crypto/rand`，因此实际单轮抽奖耗时以 `Draw` 那一行为准。
> 对 HTTP 服务而言这个量级可以忽略（一次请求的日志显示总耗时仍在微秒级）。

逐项贡献（$k$ 为特殊号码出现次数）：

| $k$ | 多项式系数之和 | 该 $k$ 的贡献 | 实测占比（80 万轮） |
| --- | --- | --- | --- |
| 2 | 5 040 | 0.0015891 | 0.001656 |
| 3 | 83 790 | 0.0761501 | 0.075795 |
| 4 | 16 590 | 0.0279374 | 0.027809 |
| 5 | 2 401 | 0.0049946 | 0.005071 |
| 6 | 343 | 0.0005876 | 0.000586 |
| **合计** | | **0.1113053** | **0.110963** |

> 这些数字不是估算，而是由单元测试强制保证的：
> `TestAcceptProbAgainstExactEnumeration` 穷举全部票数向量逐位比对，
> `TestAcceptProbPerKAgainstSamples` 用 40 万条真实随机序列逐 $k$ 校验。

### 结果分布的含义

由于约束的存在，**返回结果里各号码的占比不等于单次权重**。实测（5000 轮）：

| 号码 | 单次权重 | 结果中实际占比 |
| --- | --- | --- |
| 4（特殊） | 15% | **37.15%** |
| 1、2、3、5、6、7、8 | 各 12.14% | 各约 9.0% |

因为特殊号码每轮必然至少赢一次，它在结果中的占比被条件化抬高了。这是约束的必然结果，不是 bug。

### 兜底构造

若重抽超过 `MAX_ATTEMPTS` 仍未命中，引擎不再重抽，而是**直接构造**一个满足约束的序列：

1. 找最小的 $k$，使得「特殊号码出现 $k$ 次、其他号码各最多 $k-1$ 次」能填满 $N$ 个位置，
   即 $N-k \le m \cdot (k-1)$。默认参数下 $k=2$。
2. 填入 $k$ 个特殊号码，剩下的位置轮流分配给其他号码。
3. 打乱顺序，保证位置也是随机的。

同时 `Fallback=true` 会出现在响应里，服务端也会打 `warn` 日志——**兜底一旦出现就说明参数过紧，应当告警**。

### 关于可行性

只要号码池至少有 2 个号码，就一定存在可行的 $k$（极端情况 $k=N$：特殊号码占满全部位置，其他号码 0 次）。
所以「无解」只可能来自非法配置，而它已经被 `config.Validate` 在启动阶段拦下。
`/api/meta` 仍会返回 `feasible` 字段，供前端提前提示。

---

## 🎮 三种玩法模式

请求时通过 `scenario` 参数选择，无需重启服务。

| 模式 | `scenario` | 特殊号码权重 | 整轮约束 | 适用场景 |
| --- | --- | --- | --- | --- |
| 挑战模式（默认） | `challenging` | 15% | ✅ 开启 | 需求给定的玩法：票型固定、其余随机 |
| 等概率模式 | `fair` | 1/池大小 | ❌ 关闭 | **真正公平**的等概率抽奖 |
| 加权模式 | `hot` | 30% | ❌ 关闭 | 特殊号码更容易出现，但票型不定 |

---

## 📡 API 接口

所有接口都支持 `GET`（`/api/draw` 也支持 `POST`）。响应统一为 JSON，时间戳为毫秒。

### `GET /healthz`

存活探针，返回纯文本 `ok`。

### `GET /api/draw`

抽奖。默认 1 轮。

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `count` | int | 1 | 一次请求抽几轮，1..100 |
| `scenario` | string | `challenging` | 玩法模式，见上表 |
| `drawCount` | int | 服务端配置 | 覆盖「一轮抽多少次」，1..1000 |
| `maxNumber` | int | 服务端配置 | 覆盖号码池上界 |
| `specialProb` | float | 模式默认 | 覆盖特殊号码单次权重，0..1 |

```bash
curl -s "http://localhost:8080/api/draw" | python3 -m json.tool
```

```json
{
  "id": "746c9fca299a343599afbbc15e9b212d",
  "timestamp": 1791811339220,
  "elapsedMs": 0.512,
  "scenario": {
    "name": "challenging",
    "title": "挑战模式（默认）",
    "specialProb": 0.15,
    "constrained": true
  },
  "config": {
    "drawCount": 9,
    "specialNumber": 4,
    "specialProb": 0.15,
    "minNumber": 1,
    "maxNumber": 8,
    "poolSize": 8,
    "maxAttempts": 5000,
    "constrained": true
  },
  "items": [4, 1, 5, 3, 6, 4, 6, 4, 4],
  "stats": {
    "Counts": { "1": 1, "3": 1, "4": 4, "5": 1, "6": 2 },
    "SpecialCount": 4,
    "TopNumber": 4,
    "TopCount": 4
  },
  "attempts": 30,
  "fallback": false
}
```

批量请求（`count>1`）会额外返回 `summary` 与 `draws`：

```json
{
  "summary": { "rounds": 5, "averageAttempt": 7.8, "fallbackRounds": 0 },
  "draws": [ { "items": [...], "attempts": 3 }, ... ]
}
```

### `GET /api/meta`

返回生效配置、全部模式、权重表，以及自检指标。前端首屏用它渲染。

```json
{
  "config": { "...": "同 draw 响应" },
  "modes": [ { "name": "challenging", "title": "挑战模式（默认）", "...": "..." } ],
  "weights": [ { "number": 1, "weight": 0.1214, "isSpecial": false }, "..." ],
  "acceptProb": 0.11130534375259925,
  "expectedRounds": 8.984294610532997,
  "feasible": true,
  "uptimeSeconds": 42,
  "serverTime": 1791811339220
}
```

### `GET /api/simulate`

蒙特卡洛自检：跑若干轮，返回实测分布与理论值对照。

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `rounds` | int | 2000 | 模拟轮数，1..20000 |

```bash
curl -s "http://localhost:8080/api/simulate?rounds=5000" | python3 -m json.tool
```

关键字段：`acceptProbTheory`（解析命中率）、`averageAttempt`（实测平均重抽轮数）、
`specialTopRate`（特殊号码成为唯一最高票的比例，约束模式下恒为 1）、`distribution`（各号码实测占比）。

### 错误响应

```json
{ "error": "count 必须在 1..100 之间，收到 0", "status": 400 }
```

| 状态码 | 含义 |
| --- | --- |
| 400 | 参数非法（类型错误 / 越界 / 未知模式） |
| 404 | 路径不存在 |
| 405 | 方法不允许 |
| 500 | 服务内部错误或配置无解 |

---

## ⚙️ 配置

全部通过环境变量覆盖，无需改代码。非法值会被 `config.Validate` 在启动阶段拦下并打印原因。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | HTTP 监听端口 |
| `DRAW_COUNT` | `9` | 一轮抽多少次 |
| `SPECIAL_NUMBER` | `4` | 特殊号码（必须落在号码池内） |
| `SPECIAL_PROB` | `0.15` | 特殊号码单次权重 |
| `MIN_NUMBER` | `1` | 号码池下界 |
| `MAX_NUMBER` | `8` | 号码池上界 |
| `MAX_ATTEMPTS` | `5000` | 内部整轮重抽上限，超限启用兜底 |
| `CONSTRAINED` | `true` | 是否启用「特殊号码必须唯一最多」的整轮约束 |

```bash
# 换个玩法：号码池 1..10，特殊号码 7，权重 25%
DRAW_COUNT=11 SPECIAL_NUMBER=7 MAX_NUMBER=10 SPECIAL_PROB=0.25 go run .
```

> **调参提示**：`SPECIAL_PROB` 越低，单轮自然命中率越低，重抽越多。
> 启动后看 `/api/meta` 的 `acceptProb` 与 `expectedRounds`，或者在控制台点「概率自检」，
> 确认期望重抽轮数在你的延迟预算内。

---

## 🖥️ 内置控制台

访问 `/` 可以看到一个零依赖的单页界面：

- 号码格滚动动画，特殊号码金色高亮，锁定后才揭晓真实结果（**结果在动画开始前就已从服务端拿到**，动画只负责表现，不参与决定）。
- 实时票数统计、抽奖 ID、内部重抽轮数、服务端与往返耗时。
- 参数滑块（权重、轮内次数、号码池上界）与三种玩法切换，改动立即生效。
- 「概率自检」按钮会调用 `/api/simulate` 并展示实测分布 vs 理论值。
- 快捷键：<kbd>空格</kbd> 抽一次。

前端为原生 JS，无框架、无外部资源，全部编译进二进制（`go:embed`），断网也能用。

---

## 🛠️ 开发

```bash
make help        # 查看全部命令（Windows 无 make 时直接看 Makefile 注释）

make run         # 本地启动
make test        # 跑测试
make test-race   # 带竞态检测
make cover       # 生成 coverage.html
make check       # 提交前自检：gofmt + vet + test
make bench       # 基准测试
```

等价的原生命令：

```bash
gofmt -l .          # 应为空
go vet ./...
go test ./...
go test -bench=. -benchmem ./internal/engine/
```

### 项目结构

```
.
├── main.go                        # 服务入口：加载配置、启动 HTTP、优雅关闭
├── cmd/draw/main.go               # 命令行抽奖工具（不依赖 HTTP）
├── internal/
│   ├── config/config.go           # 配置加载（环境变量）与合法性校验
│   ├── randsource/randsource.go   # 随机源：crypto/rand 实现 + 可复现的测试实现
│   ├── engine/engine.go           # 核心算法：加权池、整轮约束、兜底、解析概率
│   ├── engine/*_test.go           # 不变量、概率公式、并发、确定性测试
│   ├── scenario/scenario.go       # 三种玩法模式的元数据与参数派生
│   └── httpapi/                   # HTTP 层：路由、中间件、DTO、内置前端
│       ├── server.go
│       ├── dto.go
│       ├── assets.go
│       └── web/                   # 编译进二进制的控制台页面
├── Dockerfile / docker-compose.yml
├── Makefile
└── .github/workflows/ci.yml       # gofmt + vet + 竞态测试 + 覆盖率 + 冒烟 + 镜像构建
```

### 为什么随机源这样设计

```go
// ❌ 早期写法：播种一次 math/rand，之后无限复用
r := mrand.New(mrand.NewSource(seedFromCrypto))
// math/rand 只有 607 个状态字，长期运行后可被反推 → 抽奖结果可被玩家操纵

// ✅ 现在：每次取随机性都直接问 crypto/rand
func (c Crypto) Float64() float64 {
    var b [8]byte
    crand.Read(b[:])                       // 每次都是新的熵
    return float64(binary.LittleEndian.Uint64(b[:])>>11) / (1 << 53)
}
```

另外，`engine` 只依赖 `randsource.Source` 接口，因此测试可以注入确定性实现（`randsource.NewSeeded`），
生产则用 `randsource.New()`——**可复现测试 + 不可预测线上**。
`Intn` 使用拒绝采样消除取模偏置，`Shuffle` 使用 Fisher–Yates。

---

## ❓ FAQ

**Q：为什么不用均匀随机？**
A：因为需求要求「特殊号码必须是唯一最高票」。均匀随机无法保证这一点，只能靠整轮重抽来筛。
这必然引入偏差——请参阅开头的诚实说明。

**Q：重抽会不会很慢？**
A：默认参数单轮自然命中率约 11.13%，平均重抽 8.98 轮，单次抽奖约 8–10 µs（含 9 次 `crypto/rand` 读取）。
即便把 `SPECIAL_PROB` 调到 0.01（命中率约 7e-5），也有 `MAX_ATTEMPTS` 兜底，不会挂起。

**Q：会不会出现某个号码永远抽不到？**
A：不会。`TestPickCoversAllNumbers` 会验证号码池内每个号码都能被抽到。
累积概率表的末位被强制设为 1.0，避免浮点误差导致尾部样本落空。

**Q：结果可复现吗？**
A：生产模式不可复现（每次请求都取新熵，这是刻意的）。
测试模式（`randsource.NewSeeded`）完全可复现，用于回归测试与审计对账。

**Q：怎么把特殊号码换成别的？**
A：`SPECIAL_NUMBER=7 go run .`。号码池、轮内次数、权重同理。

**Q：能对外宣称「公平随机」吗？**
A：**默认模式不能。** 如果想对外声称公平，请用 `scenario=fair`，并如实说明规则。

---

## 📄 License

[MIT](LICENSE)
