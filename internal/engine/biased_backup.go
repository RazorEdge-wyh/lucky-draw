package engine

import (
	"log"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/randsource"
)

// ===========================================================================
// 原始算法备份（有偏模式）
//
// 这个文件是一份**自包含的快照**：把项目最初那套「加权抽样 + 整轮必中」
// 的算法原样封在这里，不依赖 Engine 的内部字段，可以独立阅读、独立运行。
//
// 什么时候会用到它：
//   - 部署时设置 SPECIAL_NUMBER=<号码池内的号码> 即启用有偏模式，
//     此时 engine.DrawSequence 会走回这条老路径；
//   - 想对照两种算法的差异时，可以直接调用 BiasedGenerator。
//
// 它与当前默认的均匀随机算法（DrawUniform）是**互相独立**的两套实现，
// 因此改动其中一套不会影响另一套。
//
// 算法回顾：
//  1. 加权抽样：特殊号码权重 SpecialProb，其余号码均分 1-SpecialProb，抽 N 次；
//  2. 整轮校验：特殊号码必须出现过，且票数严格多于其他任何号码；
//  3. 不满足则整轮作废重抽，最多 MaxAttempts 次；
//  4. 超限后构造式兜底，保证永不挂起。
//
// 默认参数（N=9、号码 1..8、特殊号码 4、权重 0.15）下单轮自然命中率约 11.13%，
// 平均重抽约 8.98 轮；生成结果里 4 的占比约 37%（其他号码各约 9%）。
// ===========================================================================

// BiasedGenerator 是原始有偏算法的独立实现。
//
// 与 Engine 的区别：它自己持有号码池与累积概率表，不共享任何状态，
// 因此可以拿来做 A/B 对比，也可以整体拷走单独使用。
type BiasedGenerator struct {
	cfg     config.Config
	src     randsource.Source
	numbers []int
	cum     []float64
	others  []int
	per     float64
}

// BiasedResult 是一轮有偏抽取的结果。
type BiasedResult struct {
	// Items 是号码序列，长度等于配置的 DrawCount。
	Items []int
	// Attempts 是内部实际重抽的轮数（>=1）。
	Attempts int
	// Fallback 表示结果由兜底构造产生，而非自然命中。
	// 正常概率下永远不会为 true；为 true 说明参数过紧，应当告警。
	Fallback bool
}

// NewBiasedGenerator 构造原始算法的生成器。
//
// 要求 cfg.SpecialNumber 落在号码池内（即 cfg.Weighted() 为 true）；
// 否则返回 ErrNotWeighted，避免调用方误以为它做的是均匀随机。
func NewBiasedGenerator(cfg config.Config) (*BiasedGenerator, error) {
	return NewBiasedGeneratorWithSource(cfg, randsource.New())
}

// NewBiasedGeneratorWithSource 允许注入随机源，测试用确定性源。
func NewBiasedGeneratorWithSource(cfg config.Config, src randsource.Source) (*BiasedGenerator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Weighted() {
		return nil, ErrNotWeighted
	}
	if src == nil {
		return nil, ErrNilSource
	}

	others := make([]int, 0, cfg.PoolSize())
	for n := cfg.MinNumber; n <= cfg.MaxNumber; n++ {
		if n != cfg.SpecialNumber {
			others = append(others, n)
		}
	}
	if len(others) == 0 {
		return nil, ErrNotWeighted
	}

	g := &BiasedGenerator{
		cfg:    cfg,
		src:    src,
		others: others,
		per:    (1.0 - cfg.SpecialProb) / float64(len(others)),
	}

	acc := 0.0
	for n := cfg.MinNumber; n <= cfg.MaxNumber; n++ {
		w := g.per
		if n == cfg.SpecialNumber {
			w = cfg.SpecialProb
		}
		acc += w
		g.numbers = append(g.numbers, n)
		g.cum = append(g.cum, acc)
	}
	// 抹掉浮点累加误差，保证尾部样本一定落到表内
	g.cum[len(g.cum)-1] = 1.0

	return g, nil
}

// pick 按累积概率做一次加权抽样。
func (g *BiasedGenerator) pick() int {
	x := g.src.Float64()
	for i, c := range g.cum {
		if x < c {
			return g.numbers[i]
		}
	}
	return g.numbers[len(g.numbers)-1]
}

// accept 判定：特殊号码必须出现过，且严格多于其他任何号码。
func (g *BiasedGenerator) accept(counts map[int]int) bool {
	c := counts[g.cfg.SpecialNumber]
	if c == 0 {
		return false
	}
	for _, n := range g.others {
		if counts[n] >= c {
			return false
		}
	}
	return true
}

// round 执行一轮完整抽取。
func (g *BiasedGenerator) round() ([]int, map[int]int) {
	items := make([]int, g.cfg.DrawCount)
	counts := make(map[int]int, g.cfg.PoolSize())
	for i := range items {
		n := g.pick()
		items[i] = n
		counts[n]++
	}
	return items, counts
}

// FeasibleK 返回最小的 k，使得特殊号码出现 k 次时可以唯一压过其他所有号码。
// 条件：N-k <= len(others)*(k-1)。ok 为 false 表示该参数组合无解。
func (g *BiasedGenerator) FeasibleK() (int, bool) {
	n := g.cfg.DrawCount
	for k := 1; k <= n; k++ {
		if n-k <= len(g.others)*(k-1) {
			return k, true
		}
	}
	return 0, false
}

// fallback 兜底：直接构造一个满足约束的序列。
func (g *BiasedGenerator) fallback() ([]int, error) {
	n := g.cfg.DrawCount
	k, ok := g.FeasibleK()
	if !ok {
		return nil, ErrUnreachable
	}

	items := make([]int, 0, n)
	for i := 0; i < k; i++ {
		items = append(items, g.cfg.SpecialNumber)
	}

	used := make(map[int]int, len(g.others))
	for len(items) < n {
		placed := false
		for _, cand := range g.others {
			if used[cand] < k-1 {
				used[cand]++
				items = append(items, cand)
				placed = true
				break
			}
		}
		if !placed {
			break
		}
	}
	if len(items) != n {
		return nil, ErrUnreachable
	}

	g.src.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	return items, nil
}

// Draw 抽一轮，返回满足约束的序列。
//
// 重抽带硬上限；超限后走构造式兜底，因此接口永不挂起。
func (g *BiasedGenerator) Draw() *BiasedResult {
	for i := 1; i <= g.cfg.MaxAttempts; i++ {
		items, counts := g.round()
		if g.accept(counts) {
			return &BiasedResult{Items: items, Attempts: i}
		}
	}

	items, err := g.fallback()
	if err != nil {
		// 参数自相矛盾，构造不出合法结果。这是配置事故，不是运行事故。
		log.Printf("warn: 有偏算法兜底构造失败: %v", err)
		return &BiasedResult{Fallback: true, Attempts: g.cfg.MaxAttempts}
	}
	return &BiasedResult{Items: items, Attempts: g.cfg.MaxAttempts, Fallback: true}
}

// DrawSequence 抽一轮并直接返回号码切片。
func (g *BiasedGenerator) DrawSequence() []int {
	res := g.Draw()
	if len(res.Items) == 0 {
		items := make([]int, g.cfg.DrawCount)
		for i := range items {
			items[i] = g.cfg.SpecialNumber
		}
		return items
	}
	return res.Items
}

// Weights 返回每个号码的单次权重，按号码升序。
func (g *BiasedGenerator) Weights() []float64 {
	w := make([]float64, 0, len(g.numbers))
	for _, n := range g.numbers {
		if n == g.cfg.SpecialNumber {
			w = append(w, g.cfg.SpecialProb)
		} else {
			w = append(w, g.per)
		}
	}
	return w
}
