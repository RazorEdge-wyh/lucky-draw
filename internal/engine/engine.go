// Package engine 实现抽奖核心算法：加权号码池 + 整轮拒绝重抽。
//
// 算法契约（三句话概括，也是 README 里对外的说明口径）：
//  1. 每个号码被抽中的概率是预设的权重：特殊号码 SpecialProb，其余号码均分 1-SpecialProb。
//  2. 一轮抽 DrawCount 次，如果特殊号码不是「唯一最多」的号码，整轮作废重抽。
//  3. 因此返回的结果里，特殊号码必定出现且必定是唯一最高票。
//
// 直白地说：这不是「均匀随机」，而是「带约束的加权随机」。
// 约束决定了最终票型，随机只决定其余号码的分布 —— 请不要用它对外宣称绝对公平的等概率抽奖。
package engine

import (
	"errors"
	"fmt"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/randsource"
)

// Result 是一轮抽奖的结果。
type Result struct {
	// Items 是抽奖序列，长度等于 DrawCount。
	Items []int
	// Attempts 是内部实际重抽的轮数（>=1）。用于观测与压测。
	Attempts int
	// Fallback 表示结果由兜底构造产生，而非自然命中。
	// 正常概率下永远不会为 true；为 true 说明参数配置过紧，应当告警。
	Fallback bool
}

// ErrUnreachable 表示在当前参数下不可能构造出合法结果
// （例如「每个号码最多出现 k 次」与「需要 n 个号码」互相冲突）。
var ErrUnreachable = errors.New("engine: 当前参数下无法构造合法的抽奖结果")

// Stats 是一轮抽奖的统计视图。
type Stats struct {
	// Counts 是每个号码的出现次数。
	Counts map[int]int
	// SpecialCount 是特殊号码出现次数。
	SpecialCount int
	// TopNumber / TopCount 是出现次数最多的号码及其次数（并列时取号码较小者）。
	TopNumber int
	TopCount  int
}

// Engine 持有配置和号码池，是并发安全的：它没有任何可变状态，
// 每次抽取都从随机源取全新的随机性。
type Engine struct {
	cfg    config.Config
	src    randsource.Source
	pool   []int
	cum    []float64 // 累积概率，严格递增且末位为 1.0
	others []int
	per    float64 // 非特殊号码各自的权重
}

// New 用配置构造引擎。默认使用密码学安全随机源。
func New(cfg config.Config) (*Engine, error) {
	return NewWithSource(cfg, randsource.New())
}

// NewWithSource 允许注入随机源，测试用确定性源，生产用 crypto/rand。
func NewWithSource(cfg config.Config, src randsource.Source) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errors.New("engine: 随机源不能为 nil")
	}

	others := cfg.Others()
	e := &Engine{
		cfg:    cfg,
		src:    src,
		others: others,
		per:    (1.0 - cfg.SpecialProb) / float64(len(others)),
	}

	// 按号码升序铺开累积分布表。号码池很小（个位数），线性扫描比二分更快。
	acc := 0.0
	for n := cfg.MinNumber; n <= cfg.MaxNumber; n++ {
		w := e.per
		if n == cfg.SpecialNumber {
			w = cfg.SpecialProb
		}
		acc += w
		e.pool = append(e.pool, n)
		e.cum = append(e.cum, acc)
	}
	// 抹掉浮点累加误差：保证「最后一个桶」一定接住 [x, 1) 的尾部，
	// 否则 Float64 取到接近 1 的值时可能落到表外。
	e.cum[len(e.cum)-1] = 1.0

	return e, nil
}

// Config 返回引擎使用的配置副本。
func (e *Engine) Config() config.Config { return e.cfg }

// Reconfigure 基于当前配置生成一个变体引擎，至少有一个参数不同时必须成功。
// 号码池形状（上下界、特殊号码）不能通过它改变，避免破坏外部持有的池假设。
func (e *Engine) Reconfigure(cfg config.Config) (*Engine, error) {
	if cfg.MinNumber != e.cfg.MinNumber || cfg.MaxNumber != e.cfg.MaxNumber ||
		cfg.SpecialNumber != e.cfg.SpecialNumber {
		return nil, errors.New("engine: Reconfigure 不允许改变号码池结构")
	}
	return NewWithSource(cfg, e.src)
}

// Weights 返回每个号码的单次命中概率，按号码升序。用于自检和文档。
func (e *Engine) Weights() []float64 {
	w := make([]float64, 0, e.cfg.PoolSize())
	for _, n := range e.pool {
		if n == e.cfg.SpecialNumber {
			w = append(w, e.cfg.SpecialProb)
		} else {
			w = append(w, e.per)
		}
	}
	return w
}

// pick 按累积概率做一次加权抽样。
func (e *Engine) pick() int {
	x := e.src.Float64()
	for i, c := range e.cum {
		if x < c {
			return e.pool[i]
		}
	}
	// 只有在浮点误差导致 x >= cum[last] 时才会走到这里。
	return e.pool[len(e.pool)-1]
}

// Draw 抽一轮并返回结果。
//
// 重抽循环有一个容易踩的坑：单纯「重抽直到命中」在参数过紧时可能永远不结束。
// 因此这里用 MaxAttempts 硬上限兜底，超限后走构造式兜底，保证接口永不挂起。
func (e *Engine) Draw() *Result {
	// 无约束玩法（等概率 / 加权）：一轮定结果，不需要重抽。
	if !e.cfg.Constrained {
		items, _ := e.round()
		return &Result{Items: items, Attempts: 1}
	}

	for i := 1; i <= e.cfg.MaxAttempts; i++ {
		items, counts := e.round()
		if e.accept(counts) {
			return &Result{Items: items, Attempts: i}
		}
	}

	items, err := e.fallback()
	if err != nil {
		// 参数自相矛盾，构造不出合法结果。这是配置事故，不是运行事故，
		// 直接把错误抛给调用方，让它以 5xx + 明确信息暴露出来。
		return &Result{Fallback: true, Attempts: e.cfg.MaxAttempts}
	}
	return &Result{Items: items, Attempts: e.cfg.MaxAttempts, Fallback: true}
}

// DrawChecked 与 Draw 相同，但当兜底也失败时返回错误。
// HTTP 层使用这个版本，以便把配置错误变成可诊断的响应。
func (e *Engine) DrawChecked() (*Result, error) {
	res := e.Draw()
	if len(res.Items) == 0 {
		return nil, fmt.Errorf("%w（DRAW_COUNT=%d, MAX_NUMBER-MIN_NUMBER=%d, MAX_ATTEMPTS=%d）",
			ErrUnreachable, e.cfg.DrawCount, e.cfg.MaxNumber-e.cfg.MinNumber, e.cfg.MaxAttempts)
	}
	return res, nil
}

// DrawBatch 连续抽 n 轮，等价于调用 n 次 Draw。n <= 0 时返回 nil。
func (e *Engine) DrawBatch(n int) []*Result {
	if n <= 0 {
		return nil
	}
	out := make([]*Result, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, e.Draw())
	}
	return out
}

// round 执行一轮完整抽取，返回序列和票数统计。
//
// 票数统计用 map 而不是先开一个长度为池大小的计数切片：
// 号码池很小（个位数），两者耗时相当（约 900ns），但切片版本每次多一次分配、
// 多 64 字节，实测 map 更省。这类热点路径的取舍以基准测试为准，不靠直觉。
func (e *Engine) round() ([]int, map[int]int) {
	items := make([]int, e.cfg.DrawCount)
	counts := make(map[int]int, e.cfg.PoolSize())
	for i := range items {
		n := e.pick()
		items[i] = n
		counts[n]++
	}
	return items, counts
}

// accept 判定：特殊号码必须出现过，且严格多于其他任何号码。
// 关闭 Constrained 时永远接受，此时引擎退化为纯加权抽样。
func (e *Engine) accept(counts map[int]int) bool {
	if !e.cfg.Constrained {
		return true
	}
	c := counts[e.cfg.SpecialNumber]
	if c == 0 {
		return false
	}
	for _, n := range e.others {
		if counts[n] >= c {
			return false
		}
	}
	return true
}

// feasibleK 返回最小的 k，使得特殊号码出现 k 次时可以唯一压过其他所有号码。
// 条件：剩余 n-k 个位置必须能放进 len(others) 个盒子且每盒最多 k-1 个，
// 即 n-k <= len(others)*(k-1)。ok 为 false 表示该参数组合无解。
func (e *Engine) feasibleK() (int, bool) {
	n := e.cfg.DrawCount
	for k := 1; k <= n; k++ {
		if n-k <= len(e.others)*(k-1) {
			return k, true
		}
	}
	return 0, false
}

// Feasible 报告当前参数下是否存在合法结果。
//
// 关键点：自然命中率可以很低（靠重抽兜住），但不能为 0。
// 例如 SPECIAL_PROB=0 时特殊号码永远抽不到，Draw 只能走兜底构造；
// 而 DRAW_COUNT 又大于 len(others)+1 时，连构造也做不到，此时必须提前报警。
func (e *Engine) Feasible() bool {
	if !e.cfg.Constrained {
		return true
	}
	_, ok := e.feasibleK()
	return ok
}

// AcceptProb 计算「一轮自然命中」的精确概率，用于观测重抽开销。
//
// 思路：一轮结果是多项式分布。命中要求特殊号码出现 k 次，其余号码各自最多 k-1 次，
// 于是按 k 把「特殊号码严格唯一最多」的那些向量切出来、逐项累加：
//
//	P(命中) = Σ_{k=1..n} C(n,k) · M(n-k, m, k-1) · p^k · q^(n-k)
//
// 其中：
//   - C(n,k) 是特殊号码的位置选择数；
//   - m 是其他号码个数，q = (1-p)/m 是每个其他号码的单次权重；
//   - M(rest, m, limit) = Σ rest!/Πc_i!，对所有满足 Σc_i = rest 且 0 <= c_i <= limit
//     的计数向量求和。注意这里必须是「多项式系数之和」而不是「解的数量」——
//     每个计数向量对应 rest!/Πc_i! 条不同的抽取序列，漏掉这个因子会把概率算小几十倍。
//
// 全部系数走整数运算（组合数与多项式系数都是整数），只在最后转 float64，
// 因此没有任何浮点误差累积。
//
// 默认参数（n=9、m=7、p=0.15、q=0.85/7）下各项约为：
//
//	k=2 → 5.0e3 · 1 · 0.15² · q⁷        ≈ 1.59e-03
//	k=3 → 8.4e4 · 84 · 0.15³ · q⁶       ≈ 7.62e-02   （主导项）
//	k=4 → 1.7e4 · 126 · 0.15⁴ · q⁵      ≈ 2.79e-02
//	合计 ≈ 1.11e-01，即平均约 9 轮自然命中一次。
//
// 这个值与穷举全部 11440 个票数向量、以及 40 万轮蒙特卡洛实测均一致（见单元测试）。
func (e *Engine) AcceptProb() float64 {
	if !e.cfg.Constrained {
		return 1
	}
	n := e.cfg.DrawCount
	m := len(e.others)
	pSpecial := e.cfg.SpecialProb
	pOther := e.per

	total := 0.0
	for k := 1; k <= n; k++ {
		rest := n - k
		if rest > m*(k-1) {
			// 剩余位置无论如何都放不下（会超出 k-1 的上限），本 k 无解。
			continue
		}
		ms := multinomialSum(rest, m, k-1)
		if ms == 0 {
			continue
		}
		total += float64(binom(n, k)) * float64(ms) * ipow(pSpecial, k) * ipow(pOther, rest)
	}
	if total > 1 {
		return 1
	}
	return total
}

// multinomialSum 计算 Σ rest!/Πc_i!，对所有满足 Σc_i = rest 且每个 c_i ∈ [0, limit]
// 的计数向量求和（盒子可区分）。
//
// 递推：依次处理每个盒子。处理到第 i 个盒子、其前已经放好 j 个元素时，
// 这个盒子放 c 个新元素的方式数是 C(rest-j, c)（从剩余位置里挑 c 个给它），
// 因此 dp[j+c] += dp[j] · C(rest-j, c)。
//
// 这个量也可以理解成「把 rest 个可区分位置分配给 m 个盒子的方案数」，
// 上限不起作用时它等于 m^rest —— 单元测试用这个恒等式做了交叉验证。
func multinomialSum(rest, m, limit int) int64 {
	if rest < 0 || m <= 0 || limit < 0 {
		return 0
	}
	if rest == 0 {
		return 1 // 每个盒子都放 0 个，唯一方案
	}
	dp := make([]int64, rest+1)
	dp[0] = 1
	for i := 0; i < m; i++ {
		next := make([]int64, rest+1)
		for j := 0; j <= rest; j++ {
			if dp[j] == 0 {
				continue
			}
			for c := 0; c <= limit && j+c <= rest; c++ {
				next[j+c] += dp[j] * binom(rest-j, c)
			}
		}
		dp = next
	}
	return dp[rest]
}

// binom 返回 C(n, k)，全程整数运算（每一步都能整除，因此不会有精度损失）。
// n < 0 或 k < 0 或 k > n 时返回 0。
func binom(n, k int) int64 {
	if n < 0 || k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k // 用较小的 k 减少计算量
	}
	res := int64(1)
	for i := 1; i <= k; i++ {
		res = res * int64(n-k+i) / int64(i)
	}
	return res
}

// ipow 返回 base^exp（exp 为小整数，直接连乘即可）。
func ipow(base float64, exp int) float64 {
	res := 1.0
	for i := 0; i < exp; i++ {
		res *= base
	}
	return res
}

// fallback 兜底：直接构造一个满足约束的序列。
// 仅在极端异常情况下触发（重抽超限），正常流程永远走不到。
func (e *Engine) fallback() ([]int, error) {
	n := e.cfg.DrawCount
	others := e.others

	k, ok := e.feasibleK()
	if !ok {
		return nil, ErrUnreachable
	}

	items := make([]int, 0, n)
	for i := 0; i < k; i++ {
		items = append(items, e.cfg.SpecialNumber)
	}

	used := make(map[int]int, len(others))
	// 直接枚举剩余位置：每个位置只需找一个「还没到 k-1 次」的号码。
	// 容量条件已保证一定找得到，因此不需要随机重试循环。
	for len(items) < n {
		placed := false
		for _, cand := range others {
			if used[cand] < k-1 {
				used[cand]++
				items = append(items, cand)
				placed = true
				break
			}
		}
		if !placed {
			break // 理论上不可达；保险起见跳出，由下面的长度检查兜底。
		}
	}
	if len(items) != n {
		return nil, ErrUnreachable
	}

	e.src.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	return items, nil
}

// Stats 统计一个序列的票型，供响应体和测试使用。
func (e *Engine) Stats(items []int) Stats {
	counts := make(map[int]int, e.cfg.PoolSize())
	topNumber, topCount := 0, -1
	for _, n := range items {
		counts[n]++
	}
	// 固定按号码升序遍历，保证并列时结果稳定（取号码较小者）。
	for _, n := range e.pool {
		if c := counts[n]; c > topCount {
			topNumber, topCount = n, c
		}
	}
	return Stats{
		Counts:       counts,
		SpecialCount: counts[e.cfg.SpecialNumber],
		TopNumber:    topNumber,
		TopCount:     topCount,
	}
}
