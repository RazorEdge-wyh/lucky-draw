<div align="center">

# 🎲 随机数生成器

**真随机** · 号码池 1–8，每个号码概率完全相等（各 1/8）

Go · 零依赖 · 单二进制部署

[![CI](https://github.com/RazorEdge-wyh/lucky-draw/actions/workflows/ci.yml/badge.svg)](https://github.com/RazorEdge-wyh/lucky-draw/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

---

## ✅ 这是真随机

| | |
| --- | --- |
| 号码池 | 1 – 8 |
| 一轮 | 9 次，每次点一下抽出一个号码 |
| 每个号码概率 | **完全相等，各 1/8（12.5%）** |
| 额外规则 | **没有。** 没有任何号码享有特权，也没有任何号码保证出现 |

九次抽取彼此独立，所以：

- 某个号码**一整轮都不出现**是正常现象（概率约 $(7/8)^9 \approx 30\%$）
- 一轮里**出现重复号码**也是正常的
- 随机源是 `crypto/rand`（服务端）/ `crypto.getRandomValues`（浏览器），**不是 `Math.random`**

用单元测试守住了这一点：400 轮 × 9 次抽样后，每个号码占比都必须落在 1/8 ± 0.04 内，
且 4 的占比必须**低于 20%** —— 如果哪天有人重新引入偏向，测试会立刻失败。

---

## 📦 原始算法已备份

这个项目最初是一个**有偏**的实现：一轮九次必定命中 4，且 4 必须是唯一最高票。
它现在**不再是默认行为**，但**代码和文档都完整保留**，随时可以启用或找回：

| 备份层 | 位置 | 说明 |
| --- | --- | --- |
| 独立实现 | `internal/engine/biased_backup.go` | 自包含的 `BiasedGenerator`，可单独调用 |
| 老路径 | `engine.DrawSequenceBiased()` | 引擎内保留的原方法 |
| 环境变量开关 | `SPECIAL_NUMBER=4` | 一键切回有偏模式 |
| 完整文档 | `docs/legacy-biased-algorithm.md` | 算法推导、概率表、恢复方法 |
| 测试 | `internal/engine/biased_backup_test.go` | 5 个测试持续守护老算法不被改坏 |

```bash
# 切回老的有偏模式（一轮必中 4）
SPECIAL_NUMBER=4 go run .
```

想细看的话：`go test ./internal/engine/ -run 'Biased' -v`

---

## 🚀 快速开始

这个仓库里有两份**算法完全相同**的实现，按需要选一个：

| | 静态版（`site/`） | 服务端版（Go） |
| --- | --- | --- |
| 运行位置 | 浏览器，纯前端 | Go 服务 |
| 随机源 | `crypto.getRandomValues()` | `crypto/rand` |
| 部署 | GitHub Pages（本仓库已配置） | 任意支持 Docker 的平台 |
| 适合 | 分享链接给别人用 | 嵌入自己的系统、需要隐藏算法 |

### 静态版

直接打开 <https://razoredge-wyh.github.io/lucky-draw/>，或本地起个静态服务器：

```bash
python3 -m http.server 8000 --directory site
```

### 服务端版（Go 1.22+）

```bash
git clone https://github.com/RazorEdge-wyh/lucky-draw.git
cd lucky-draw
go run .
```

打开 <http://localhost:8080> 就能用了。

### Docker

```bash
docker build -t lucky-draw .
docker run --rm -p 8080:8080 lucky-draw
```

---

## 🖥️ 界面

只有四样东西：**标题、号码、本轮结果、按钮**。没有参数面板、没有历史记录、没有日志。

- 点「抽取」-> 号码快速滚动 -> 减速定格在新的数字上
- 下方九个圆点显示本轮进度
- **本轮结果**：抽过的号码按顺序排开，抽到 4 会高亮；没抽的位置用虚线占位，
  一眼能看出还剩几次。抽满九次后给出一行小结，例如
  「本轮 9 次 · 4 出现 3 次 · 其他号码最多 2 次」，停留约两秒后自动进入下一轮
- 键盘 <kbd>空格</kbd> 也可以抽
- 自动适配浅色／深色模式，手机端可用

结果在动画开始前就已确定，**动画只负责表现，不参与决定数字**。

> 为什么只显示本轮、不显示历史？这是刻意的：只显示当前轮可以让页面保持极简，
> 也不会把「哪一轮出过什么」累积成一份可被利用的记录。

---

## 📡 接口

以下接口属于**服务端版**；静态版跑在浏览器里，没有 HTTP 接口。

只有三个，够用就好。

### `GET /api/draw`

抽下一个号码。

```bash
curl -c jar -b jar localhost:8080/api/draw
```

```json
{ "number": 4, "index": 3, "total": 9, "roundDone": false, "round": 1 }
```

| 字段 | 含义 |
| --- | --- |
| `number` | 本次抽出的号码 |
| `index` | 这是本轮第几次（1..9） |
| `total` | 一轮多少次 |
| `roundDone` | 本轮是否已抽满（第九次为 `true`） |
| `round` | 当前是第几轮 |

服务端用 `ld_sid` Cookie 记住你的进度。**不带 Cookie 每次都会开始新一轮**，
所以用 curl 连续抽的时候要带上它（浏览器会自动带）。

### `GET /api/reset`

放弃当前进度，下次抽取重新开始一轮。

### `GET /healthz`

返回 `ok`，给负载均衡和容器健康检查用。

---

## 🔒 为什么不会「抽到一半发现凑不齐」

**整轮九次的号码序列，在你第一次点击时就已经一次性生成好了。**

九次是彼此独立的等概率抽取，先生成完整一轮，再按顺序一个一个交付。
因此每一次点击都一定成功，不存在「抽到第九个才发现凑不齐」的情况。

单轮生成耗时约 8–10 µs，其中绝大部分是 9 次 `crypto/rand` 读取的系统调用开销。

> 老的有偏算法也有同样的「一次性生成」特性 —— 区别在于它生成后还要校验
> 「4 是否唯一最高票」，不满足就整轮重抽（平均重抽约 8.98 轮）。
> 细节见 `docs/legacy-biased-algorithm.md`。

---

## ⚙️ 配置

全部通过环境变量覆盖，非法值会在启动时被拦下并打印原因。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | 监听端口 |
| `DRAW_COUNT` | `9` | 一轮抽多少次 |
| `MIN_NUMBER` | `1` | 号码池下界 |
| `MAX_NUMBER` | `8` | 号码池上界 |
| `SPECIAL_NUMBER` | `0` | **`0` = 均匀随机（默认）**；设为号码池内的号码则切回有偏模式 |
| `SPECIAL_PROB` | `0.15` | 仅在有偏模式下生效：特殊号码的单次权重 |
| `MAX_ATTEMPTS` | `5000` | 仅有偏模式下生效：整轮重抽上限 |

```bash
# 默认：均匀随机，号码池 1..20，一轮 11 次
DRAW_COUNT=11 MAX_NUMBER=20 go run .

# 切回老的有偏模式：一轮必中 4，且 4 是唯一最高票
SPECIAL_NUMBER=4 SPECIAL_PROB=0.15 go run .
```

> `SPECIAL_NUMBER=0` 是「真随机」的开关。只要它非 0，服务就回到
> 「加权抽样 + 整轮必中」的老行为 —— 这是刻意保留的兼容开关。

---

## 🚢 部署

镜像很小（distroless + 静态二进制），任何支持 Docker 的平台都能跑。

```bash
# 本地 / 云服务器
docker run -d --restart unless-stopped -p 80:8080 --name lucky-draw lucky-draw
```

平台部署要点：

- **端口**：容器内监听 `8080`。多数平台用 `PORT` 环境变量注入端口，服务会自动读取。
- **健康检查路径**：`/healthz`（返回 `ok`）。
- **单实例即可**：抽奖进度存在内存里，多副本时各副本各管各的会话，无需共享状态。
- 镜像基于 `distroless/static`，**没有 shell**；健康检查由二进制自带的 `-healthcheck` 完成。

### 命令行开关

```bash
./lucky-draw -version       # 打印版本
./lucky-draw -check         # 只校验配置，不启动服务
./lucky-draw -healthcheck   # 请求本机 /healthz，成功退出 0，失败退出 1
```

---

## 🛠️ 开发

```bash
gofmt -l .                  # 应为空
go vet ./...
go test ./...               # 全部测试
go test -race ./...         # 含竞态检测
go test -bench=. -benchmem ./internal/engine/
```

### 项目结构

```
.
├── site/index.html                    # 静态版：单文件，含算法与动画（GitHub Pages 发布这个）
├── main.go                            # 服务端版入口：配置、启动、优雅关闭、-healthcheck
├── internal/
│   ├── config/config.go               # 环境变量配置与校验（含「均匀 / 有偏」模式开关）
│   ├── randsource/randsource.go       # crypto/rand 随机源（另有可复现的测试实现）
│   ├── engine/
│   │   ├── engine.go                  # 均匀随机（默认）+ 加权抽样 / 整轮约束 / 兜底 / 解析概率
│   │   ├── biased_backup.go           # ★ 原始有偏算法的独立备份实现
│   │   └── biased_backup_test.go      # 5 个测试守护备份算法
│   └── httpapi/                       # 会话进度、三个接口、内置页面
│       ├── server.go
│       ├── assets.go
│       └── web/{index.html,app.js}    # 编译进二进制
├── docs/legacy-biased-algorithm.md    # ★ 原始算法完整文档（推导 + 概率表 + 恢复方法）
├── Dockerfile / docker-compose.yml
└── .github/workflows/
    ├── ci.yml                         # 格式化 + 静态检查 + 竞态测试 + 冒烟 + 镜像构建
    └── deploy-pages.yml               # 把 site/ 发布到 GitHub Pages
```

### 两份实现的一致性

静态版不是「简化版」，而是把同一套逻辑照搬到了浏览器：同样的号码池、
同样的等概率选择、同样的拒绝采样消偏。两边都是**均匀随机**：

| 号码 | 概率 |
| --- | --- |
| 1、2、3、4、5、6、7、8 | **各 12.5%** |

唯一的区别是**算法在静态版里是公开可见的**（浏览器里跑，代码必然可读）。
服务端版可以把逻辑留在服务器上。

### 随机源

生产路径**直接调用 `crypto/rand`**，而不是「用 crypto/rand 播一次种、之后一直用 math/rand」——
后者只有 607 个状态字，长期运行后能被反推，抽取结果就可被操纵了。
浏览器端用 `crypto.getRandomValues`，同样刻意**不退回 `Math.random()`**。

引擎只依赖 `randsource.Source` 接口，所以测试可以注入确定性实现（相同种子必得相同结果），
生产则用密码学安全实现：**可复现测试 + 不可预测线上**。
`Intn` 用拒绝采样消除取模偏置（`random % n` 会让部分取值概率偏大），`Shuffle` 用 Fisher–Yates。

---

## 📄 License

[MIT](LICENSE)
