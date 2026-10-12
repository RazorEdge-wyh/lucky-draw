# lucky-draw 接口调用示例

需要服务已在运行（`go run .` 或 `docker compose up -d`）。

```bash
BASE=http://localhost:8080
```

---

## 1. 存活探针

```bash
curl -s "$BASE/healthz"
# ok
```

## 2. 抽一轮（默认玩法）

```bash
curl -s "$BASE/api/draw" | python3 -m json.tool
```

只看号码序列：

```bash
curl -s "$BASE/api/draw" | python3 -c 'import json,sys; print(json.load(sys.stdin)["items"])'
# [4, 4, 2, 1, 6, 4, 8, 4, 4]
```

## 3. 连抽 10 轮

```bash
curl -s "$BASE/api/draw?count=10" | python3 -c '
import json, sys
d = json.load(sys.stdin)
print("平均重抽 %.2f 轮，兜底 %d 轮" % (d["summary"]["averageAttempt"], d["summary"]["fallbackRounds"]))
for i, r in enumerate(d["draws"], 1):
    print("第 %2d 轮 %s  重抽 %d 轮" % (i, r["items"], r["attempts"]))
'
```

## 4. 查看生效配置与权重表

```bash
curl -s "$BASE/api/meta" | python3 -c '
import json, sys
m = json.load(sys.stdin)
c = m["config"]
print("一轮 %d 次，号码池 %d..%d，特殊号码 %d" % (c["drawCount"], c["minNumber"], c["maxNumber"], c["specialNumber"]))
print("单轮自然命中率 %.6f → 期望重抽 %.2f 轮" % (m["acceptProb"], m["expectedRounds"]))
for w in m["weights"]:
    print("  %s号码 %d：%.4f" % ("*" if w["isSpecial"] else " ", w["number"], w["weight"]))
'
```

## 5. 概率自检（实测 vs 理论）

```bash
curl -s "$BASE/api/simulate?rounds=20000" | python3 -c '
import json, sys
s = json.load(sys.stdin)
print("模拟 %d 轮" % s["rounds"])
print("理论命中率 %.5f，实测平均重抽 %.3f 轮（期望 %.3f）" % (
    s["acceptProbTheory"], s["averageAttempt"], s["expectedRounds"]))
print("特殊号码最高票比例 %.4f" % s["specialTopRate"])
for d in s["distribution"]:
    print("  %s号码 %d 占比 %.4f" % ("*" if d["isSpecial"] else " ", d["number"], d["share"]))
'
```

## 6. 切换玩法模式

```bash
# 真正等概率（无整轮约束）
curl -s "$BASE/api/draw?scenario=fair" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["items"], "重抽", d["attempts"], "轮")'

# 加权模式：特殊号码 30%，不保证最高票
curl -s "$BASE/api/draw?scenario=hot" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["items"], "重抽", d["attempts"], "轮")'
```

## 7. 临时覆盖参数

```bash
# 特殊号码权重提到 50%
curl -s "$BASE/api/draw?specialProb=0.5" | python3 -c 'import json,sys; print(json.load(sys.stdin)["config"]["specialProb"])'

# 号码池扩到 1..12，一轮抽 13 次
curl -s "$BASE/api/draw?maxNumber=12&drawCount=13" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["items"], "池大小", d["config"]["poolSize"])'
```

## 8. 错误处理

```bash
# 非法参数：返回 400 并说明原因
curl -s -w '\nHTTP %{http_code}\n' "$BASE/api/draw?count=0"
# {"error":"count 必须在 1..100 之间，收到 0","status":400}
# HTTP 400

# 未知模式
curl -s -w '\nHTTP %{http_code}\n' "$BASE/api/draw?scenario=nope"
```

## 9. 压测

```bash
# 用 hey 或 ab；也可以用项目自带的基准测试
go run ./cmd/draw -bench

# 并发压测 2000 次请求
seq 2000 | xargs -P 32 -I{} curl -s -o /dev/null "$BASE/api/draw"
```

或者直接跑 Go 基准：

```bash
go test -bench=. -benchmem ./internal/engine/
```

## 10. 命令行直接抽奖（不起服务）

```bash
go run ./cmd/draw -n 5
go run ./cmd/draw -json | python3 -m json.tool
go run ./cmd/draw -simulate 100000
```
