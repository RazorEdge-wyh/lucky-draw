<div align="center">

# 🎲 随机数生成器

**点一下，抽一个数字。九次为一轮，一轮一定命中 4。**

Go · 零依赖 · 单二进制部署

[![CI](https://github.com/RazorEdge-wyh/lucky-draw/actions/workflows/ci.yml/badge.svg)](https://github.com/RazorEdge-wyh/lucky-draw/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

</div>

---

## ⚠️ 关于「随机」的说明

这个工具叫随机数生成器，但它**不是均匀等概率抽奖**。规则是：

| | |
| --- | --- |
| 号码池 | 1 – 8 |
| 一轮 | 九次，**每次点一下抽出一个数字** |
| 特殊号码 | **4** |
| 本轮保证 | 一轮九次抽完，**4 一定出现过，而且票数严格多于其他任何号码** |

也就是说：**4 是否「中奖」由规则决定，不是随机决定的；随机只决定其余号码怎么分布。**

常见票型举例（4 的票数总是最高的那个）：

```
4,4,8,4,7,6,1,6,3     ->  4 出现 3 次，其他最多 2 次
2,1,8,7,4,3,4,8,4     ->  4 出现 3 次，其他最多 2 次
4,4,4,5,7,5,4,4,1     ->  4 出现 5 次，其他最多 2 次
```

如果你需要**真正等概率**的抽奖，请不要用这个项目。

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

只有三样东西：**标题、号码、按钮**。没有参数面板、没有统计、没有日志。

- 点「抽取」-> 号码快速滚动 -> 减速定格在新的数字上
- 下方九个圆点显示本轮进度，抽满九个自动开始下一轮
- 键盘 <kbd>空格</kbd> 也可以抽
- 自动适配浅色／深色模式，手机端可用

结果在动画开始前就已从服务端取回，**动画只负责表现，不参与决定数字**。

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

这是设计上最关键的一点：

**整轮九次的号码序列，在你第一次点击时就已经一次性生成并校验完了。**

约束（4 必须是唯一最高票）在生成阶段就满足，接口只是把这条已经合法的序列
按顺序一个一个交付出去。因此不可能出现「抽到第九个才发现凑不齐、只能让你重来」
的情况 —— 每一次点击都一定成功。

生成算法分两步：

1. **加权抽样**：4 的权重 15%，其余 7 个号码各约 12.14%，抽九次。
2. **整轮校验**：判断 4 是否严格多于其他每个号码；不满足就整轮作废重抽。

默认参数下单轮自然命中的概率是 **11.13%**，平均重抽 **8.98 轮**，
单轮生成耗时约 8–10 µs。这个概率有解析解，并由单元测试用穷举与蒙特卡洛双重验证：

```
P(命中) = Σ_{k=1..9} C(9,k) · M(9-k, 7, k-1) · 0.15^k · 0.1214^(9-k)
```

其中 `M(r,m,L) = Σ r!/Πc_i!` 是对所有 `Σc_i = r`、`0 <= c_i <= L` 的计数向量求和
（用整数动态规划精确计算，不引入浮点误差）。

万一重抽超过上限（`MAX_ATTEMPTS`，默认 5000），引擎会直接**构造**一个合法序列兜底，
保证接口永不挂起。

---

## ⚙️ 配置

全部通过环境变量覆盖，非法值会在启动时被拦下并打印原因。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | 监听端口 |
| `DRAW_COUNT` | `9` | 一轮抽多少次 |
| `SPECIAL_NUMBER` | `4` | 特殊号码（必须在本轮成为唯一最高票） |
| `SPECIAL_PROB` | `0.15` | 特殊号码单次权重 |
| `MIN_NUMBER` | `1` | 号码池下界 |
| `MAX_NUMBER` | `8` | 号码池上界 |
| `MAX_ATTEMPTS` | `5000` | 整轮重抽上限，超限启用兜底构造 |

```bash
# 例：号码池 1..10，特殊号码 7，一轮十一次
DRAW_COUNT=11 SPECIAL_NUMBER=7 MAX_NUMBER=10 PORT=9000 go run .
```

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
│   ├── config/config.go               # 环境变量配置与校验
│   ├── randsource/randsource.go       # crypto/rand 随机源（另有可复现的测试实现）
│   ├── engine/engine.go               # 加权抽样 + 整轮约束 + 兜底 + 解析概率
│   └── httpapi/                       # 会话进度、三个接口、内置页面
│       ├── server.go
│       ├── assets.go
│       └── web/{index.html,app.js}    # 编译进二进制
├── Dockerfile / docker-compose.yml
└── .github/workflows/
    ├── ci.yml                         # 格式化 + 静态检查 + 竞态测试 + 冒烟 + 镜像构建
    └── deploy-pages.yml               # 把 site/ 发布到 GitHub Pages
```

### 两份实现的一致性

静态版不是"简化版"，而是把同一套逻辑照搬到了浏览器：同样的权重、同样的整轮校验、
同样的拒绝采样消偏、同样的兜底构造。实测后验分布一致：

| 号码 | 服务端版（Go，实测） | 静态版（JS 逻辑复算） |
| --- | --- | --- |
| 4（特殊） | 37.15% | 37.18% |
| 1、2、3、5、6、7、8 | 各约 9.0% | 各约 9.0% |

唯一的区别是**算法在静态版里是公开可见的**（浏览器里跑，代码必然可读）。
如果你需要把抽奖逻辑藏起来，请用服务端版。

### 随机源

生产路径**直接调用 `crypto/rand`**，而不是「用 crypto/rand 播一次种、之后一直用 math/rand」——
后者只有 607 个状态字，长期运行后能被反推，抽奖结果就可被操纵了。

引擎只依赖 `randsource.Source` 接口，所以测试可以注入确定性实现（相同种子必得相同结果），
生产则用密码学安全实现：**可复现测试 + 不可预测线上**。
`Intn` 用拒绝采样消除取模偏置，`Shuffle` 用 Fisher–Yates。

---

## 📄 License

[MIT](LICENSE)
