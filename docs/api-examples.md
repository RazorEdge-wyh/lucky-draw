# 接口调用示例

服务已启动（`go run .` 或 `docker compose up -d`）后：

```bash
BASE=http://localhost:8080
```

---

## 1. 抽一个数字

```bash
curl -s "$BASE/api/draw"
# {"number":4,"index":1,"total":9,"roundDone":false,"round":1}
```

## 2. 连抽九次（一轮）

**关键：必须带上 Cookie**，否则每次请求都会被当成新一轮的开始。

```bash
jar=$(mktemp)
for i in $(seq 1 9); do
  curl -s -c "$jar" -b "$jar" "$BASE/api/draw"
  echo
done
```

输出示例：

```
{"number":7,"index":1,"total":9,"roundDone":false,"round":1}
{"number":5,"index":2,"total":9,"roundDone":false,"round":1}
{"number":3,"index":3,"total":9,"roundDone":false,"round":1}
{"number":4,"index":4,"total":9,"roundDone":false,"round":1}
{"number":7,"index":5,"total":9,"roundDone":false,"round":1}
{"number":4,"index":6,"total":9,"roundDone":false,"round":1}
{"number":2,"index":7,"total":9,"roundDone":false,"round":1}
{"number":2,"index":8,"total":9,"roundDone":false,"round":1}
{"number":4,"index":9,"total":9,"roundDone":true,"round":1}
```

这一轮里 4 出现 3 次，其他号码最多 2 次 —— 4 是唯一最高票。

## 3. 校验「一轮一定命中 4」

```bash
jar=$(mktemp)
python3 -c '
import json, subprocess, sys, collections
jar = sys.argv[1]
base = "http://localhost:8080/api/draw"
items = []
for i in range(1, 10):
    out = subprocess.run(["curl","-s","-c",jar,"-b",jar,base],
                         capture_output=True, text=True, check=True).stdout
    items.append(json.loads(out)["number"])
c = collections.Counter(items)
assert c[4] > 0, "没命中 4: %s" % items
assert c[4] > max(v for k, v in c.items() if k != 4), "4 不是唯一最高票: %s" % items
print("一轮九次 =", items, "| 4 出现", c[4], "次，其他最多",
      max(v for k, v in c.items() if k != 4), "次")
' "$jar"
```

## 4. 连续多轮

同一个 Cookie 继续抽，第十次会自动开始第二轮（`round` 变成 2）：

```bash
jar=$(mktemp)
for i in $(seq 1 18); do
  curl -s -c "$jar" -b "$jar" "$BASE/api/draw"
  echo
done
```

## 5. 重置进度

```bash
curl -s -X POST "$BASE/api/reset"
# {"ok":true}
```

## 6. 健康检查

```bash
curl -s "$BASE/healthz"
# ok
```

## 7. 已下线的接口

早期版本有参数配置与统计接口，现在都已删除，请求会返回 404：

```bash
curl -s -o /dev/null -w '%{http_code}\n' "$BASE/api/meta"      # 404
curl -s -o /dev/null -w '%{http_code}\n' "$BASE/api/simulate"  # 404
```

## 8. 压测

```bash
# 每个请求都是独立的新一轮（不带 Cookie）
seq 2000 | xargs -P 32 -I{} curl -s -o /dev/null "$BASE/api/draw"
```

或者跑 Go 基准：

```bash
go test -bench=. -benchmem ./internal/engine/
```
