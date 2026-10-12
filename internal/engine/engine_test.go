package engine

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/randsource"
)

// defaultCfg 与需求给定的默认参数保持一致：
// 一轮 9 次、号码 1..8、特殊号码 4、特殊号码权重 15%。
func defaultCfg() config.Config { return config.Default() }

// newTestEngine 构造一个用确定性随机源驱动的引擎，便于复现。
func newTestEngine(t *testing.T, cfg config.Config, seed uint64) *Engine {
	t.Helper()
	e, err := NewWithSource(cfg, randsource.NewSeeded(seed))
	if err != nil {
		t.Fatalf("构造引擎失败: %v", err)
	}
	return e
}

// TestDrawAlwaysSatisfiesConstraint 是整个算法的核心不变量：
// 无论怎么抽，特殊号码都必须出现，且必须是唯一最高票。
func TestDrawAlwaysSatisfiesConstraint(t *testing.T) {
	const rounds = 20000
	e := newTestEngine(t, defaultCfg(), 20260214)

	for i := 0; i < rounds; i++ {
		res := e.Draw()
		st := e.Stats(res.Items)

		if len(res.Items) != 9 {
			t.Fatalf("第 %d 轮长度错误: got %d want 9 (%v)", i, len(res.Items), res.Items)
		}
		if st.SpecialCount == 0 {
			t.Fatalf("第 %d 轮特殊号码未出现: %v", i, res.Items)
		}
		if st.TopNumber != 4 {
			t.Fatalf("第 %d 轮最高票不是特殊号码: items=%v top=%d", i, res.Items, st.TopNumber)
		}
		for _, n := range []int{1, 2, 3, 5, 6, 7, 8} {
			if st.Counts[n] >= st.SpecialCount {
				t.Fatalf("第 %d 轮号码 %d 票数 %d 未严格小于特殊号码 %d: %v",
					i, n, st.Counts[n], st.SpecialCount, res.Items)
			}
		}
		if res.Fallback {
			t.Fatalf("第 %d 轮不该触发兜底: %v", i, res.Items)
		}
	}
}

// TestPickFollowsWeights 直接检验加权抽样本身：
// 4 号应占约 15%，其余 7 个号码各占约 (85/7)% ≈ 12.14%。
func TestPickFollowsWeights(t *testing.T) {
	const n = 400000
	e := newTestEngine(t, defaultCfg(), 7)

	hist := make(map[int]int, 8)
	for i := 0; i < n; i++ {
		hist[e.pick()]++
	}

	check := func(num int, want float64) {
		t.Helper()
		got := float64(hist[num]) / float64(n)
		// 4e5 次采样的二项分布标准差约 sqrt(p(1-p)/n) ≈ 0.0006，
		// 取 0.004 作为容忍度，既不会误报也能抓出明显错误。
		if math.Abs(got-want) > 0.004 {
			t.Errorf("号码 %d 实测占比 %.4f，期望 %.4f（偏差过大）", num, got, want)
		}
	}

	check(4, 0.15)
	per := 0.85 / 7
	for _, num := range []int{1, 2, 3, 5, 6, 7, 8} {
		check(num, per)
	}

	// 权重表本身也要自洽：总和为 1，且 4 号权重最突出。
	sum := 0.0
	for _, w := range e.Weights() {
		sum += w
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Errorf("权重之和应为 1，实际 %.12f", sum)
	}
}

// TestAcceptProbMatchesSimulation 检验解析解与蒙特卡洛实测一致。
// 这个测试保证「期望重抽轮数」这个对外展示的指标是真的。
func TestAcceptProbMatchesSimulation(t *testing.T) {
	cfg := defaultCfg()
	e := newTestEngine(t, cfg, 99)

	theory := e.AcceptProb()
	if theory <= 0 || theory >= 1 {
		t.Fatalf("单轮命中率应在 (0,1) 内，实际 %v", theory)
	}

	// 用独立随机源统计「一轮自然命中」的频率。
	probe := newTestEngine(t, cfg, 12345)
	const rounds = 150000
	hits := 0
	for i := 0; i < rounds; i++ {
		items, counts := probe.round()
		_ = items
		if probe.accept(counts) {
			hits++
		}
	}

	measured := float64(hits) / float64(rounds)
	if math.Abs(measured-theory) > 0.005 {
		t.Errorf("实测命中率 %.4f 与解析值 %.4f 偏差过大", measured, theory)
	}

	// 期望重抽轮数应约等于 1/p，且实测平均重抽轮数应落在同一量级。
	wantRounds := 1 / theory
	sum := 0
	for i := 0; i < 20000; i++ {
		sum += probe.Draw().Attempts
	}
	gotRounds := float64(sum) / 20000
	if math.Abs(gotRounds-wantRounds)/wantRounds > 0.10 {
		t.Errorf("实测平均重抽 %.2f 轮，理论 %.2f 轮，偏差超过 10%%", gotRounds, wantRounds)
	}
}

// TestFallbackProducesValidResult 强制走兜底分支：
// 把 MaxAttempts 设为 1 且权重为 0，自然命中概率为 0，必然进入构造逻辑。
func TestFallbackProducesValidResult(t *testing.T) {
	cfg := defaultCfg()
	cfg.SpecialProb = 0 // 特殊号码永远抽不到
	cfg.MaxAttempts = 1 // 立刻触发兜底

	e := newTestEngine(t, cfg, 4242)
	if !e.Feasible() {
		t.Fatal("默认参数下兜底构造应当可行")
	}

	for i := 0; i < 200; i++ {
		res := e.Draw()
		if len(res.Items) == 0 {
			t.Fatalf("第 %d 轮兜底失败", i)
		}
		if !res.Fallback {
			t.Fatalf("第 %d 轮应标记为兜底", i)
		}
		st := e.Stats(res.Items)
		if st.SpecialCount == 0 {
			t.Fatalf("兜底结果里特殊号码必须出现: %v", res.Items)
		}
		if st.TopNumber != 4 {
			t.Fatalf("兜底结果里特殊号码必须是唯一最高票: %v %+v", res.Items, st.Counts)
		}
		// 兜底构造出的 k 必须是最小的可行值（9 个位置、7 个其他号码 → k=2）。
		if st.SpecialCount != 2 {
			t.Fatalf("兜底应取最小可行 k=2，实际特殊号码出现 %d 次: %v", st.SpecialCount, res.Items)
		}
	}
}

// TestFeasibilityAcrossPoolSizes 校验可行性的边界。
//
// 结论：只要号码池至少有 2 个号码，就一定能找到可行的 k ——
// 因为 k=n 时「特殊号码出现 n 次、其他号码 0 次」永远满足约束。
// 因此 Feasible() 只在号码池只剩特殊号码一个号码时为 false，
// 而那种配置已经被 config.Validate 提前拒绝，属于双保险。
func TestFeasibilityAcrossPoolSizes(t *testing.T) {
	// 号码池 1..2、特殊号码 1：仍然可行（k=9 时其他号码全 0 次）。
	cfg := defaultCfg()
	cfg.MinNumber, cfg.MaxNumber, cfg.SpecialNumber = 1, 2, 1
	e := newTestEngine(t, cfg, 1)
	k, ok := e.feasibleK()
	if !ok {
		t.Fatalf("号码池 1..2 应当可行")
	}
	t.Logf("号码池 1..2：最小可行 k=%d", k)

	// 数值上验证这个 k 确实满足容量条件：n-k <= m*(k-1)。
	n, m := cfg.DrawCount, len(cfg.Others())
	if n-k > m*(k-1) {
		t.Errorf("k=%d 不满足容量条件：n-k=%d > m*(k-1)=%d", k, n-k, m*(k-1))
	}
	// 且它必须是最小的：k-1 应当不满足。
	if k > 1 && n-(k-1) <= m*(k-2) {
		t.Errorf("k=%d 不是最小可行值", k)
	}

	// 号码池只有一个号码（others 为空）：构造阶段就应当被拒绝。
	cfg2 := defaultCfg()
	cfg2.MinNumber, cfg2.MaxNumber, cfg2.SpecialNumber = 4, 4, 4
	if _, err := NewWithSource(cfg2, randsource.NewSeeded(1)); err == nil {
		t.Error("号码池只有一个号码时配置应当被 Validate 拒绝")
	}
}

// TestDrawSequenceIsNineLongAndFourWins 校验提供给「一次只交付一个号码」接口的
// DrawSequence：长度等于一轮次数，且满足核心约束。
func TestDrawSequenceIsNineLongAndFourWins(t *testing.T) {
	e := newTestEngine(t, defaultCfg(), 2024)

	for i := 0; i < 300; i++ {
		seq := e.DrawSequence()
		if len(seq) != 9 {
			t.Fatalf("第 %d 轮序列长度应为 9，实际 %d：%v", i, len(seq), seq)
		}
		st := e.Stats(seq)
		if st.SpecialCount == 0 {
			t.Fatalf("第 %d 轮没有命中 4：%v", i, seq)
		}
		if st.TopNumber != 4 {
			t.Fatalf("第 %d 轮 4 不是唯一最高票：%v", i, seq)
		}
		for n, c := range st.Counts {
			if n != 4 && c >= st.SpecialCount {
				t.Fatalf("第 %d 轮号码 %d 票数不严格小于 4：%v", i, n, seq)
			}
		}
		// 每个号码都必须落在号码池内。
		for _, n := range seq {
			if n < 1 || n > 8 {
				t.Fatalf("第 %d 轮号码越界：%d", i, n)
			}
		}
	}
}

// TestDeterministicWithSameSeed 相同种子必须产生完全相同的结果，
// 这是「可复现」承诺的基础（也用于事后审计对账）。
func TestDeterministicWithSameSeed(t *testing.T) {
	const rounds = 50
	e1 := newTestEngine(t, defaultCfg(), 555)
	e2 := newTestEngine(t, defaultCfg(), 555)

	for i := 0; i < rounds; i++ {
		r1, r2 := e1.Draw(), e2.Draw()
		if fmt.Sprint(r1.Items) != fmt.Sprint(r2.Items) {
			t.Fatalf("第 %d 轮结果不一致: %v vs %v", i, r1.Items, r2.Items)
		}
		if r1.Attempts != r2.Attempts {
			t.Fatalf("第 %d 轮重抽次数不一致: %d vs %d", i, r1.Attempts, r2.Attempts)
		}
	}

	// 不同种子应当产生不同序列（否则随机源没有真正被使用）。
	e3 := newTestEngine(t, defaultCfg(), 556)
	same := 0
	for i := 0; i < rounds; i++ {
		if fmt.Sprint(e1.Draw().Items) == fmt.Sprint(e3.Draw().Items) {
			same++
		}
	}
	// 单轮序列完全相同（含重抽路径）的概率极低，出现多次说明随机源有问题。
	if same > rounds/10 {
		t.Errorf("%d/%d 轮在不同种子下结果相同，随机源可能失效", same, rounds)
	}
}

// TestCryptoSourceCoversRange 检验生产随机源的取值范围与覆盖度。
func TestCryptoSourceCoversRange(t *testing.T) {
	src := randsource.New()

	// Float64 必须严格落在 [0,1)。
	for i := 0; i < 20000; i++ {
		f := src.Float64()
		if f < 0 || f >= 1 {
			t.Fatalf("Float64 越界: %v", f)
		}
	}

	// Intn(n) 必须覆盖 0..n-1 且不越界。
	const n = 6
	seen := make(map[int]bool, n)
	for i := 0; i < 20000; i++ {
		v := src.Intn(n)
		if v < 0 || v >= n {
			t.Fatalf("Intn(%d) 越界: %d", n, v)
		}
		seen[v] = true
	}
	if len(seen) != n {
		t.Fatalf("Intn(%d) 未覆盖全部取值: %v", n, seen)
	}
}

// TestEngineIsConcurrencySafe 检验生产引擎能被多协程共享。
//
// HTTP 层是多协程的，因此这一点必须成立。注意这里刻意使用生产随机源
// randsource.New()：确定性源 randsource.Seeded 内部有可变状态、本身不是
// 并发安全的，拿它跑并发测试会被竞态检测器正确抓住（那是测试写错了，
// 不是引擎有问题）。Crypto 没有内部状态，可以安全共享。
func TestEngineIsConcurrencySafe(t *testing.T) {
	concEngine, err := NewWithSource(defaultCfg(), randsource.New())
	if err != nil {
		t.Fatalf("构造并发测试引擎失败: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				res, err := concEngine.DrawChecked()
				if err != nil {
					errs <- err
					return
				}
				if st := concEngine.Stats(res.Items); st.TopNumber != 4 {
					errs <- fmt.Errorf("并发下约束被破坏: %v", res.Items)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// TestDrawBatchAndStats 检验批量接口与统计口径。
func TestDrawBatchAndStats(t *testing.T) {
	e := newTestEngine(t, defaultCfg(), 31337)

	if got := e.DrawBatch(0); got != nil {
		t.Errorf("DrawBatch(0) 应返回 nil，实际 %v", got)
	}
	if got := e.DrawBatch(-3); got != nil {
		t.Errorf("DrawBatch(-3) 应返回 nil，实际 %v", got)
	}

	batch := e.DrawBatch(10)
	if len(batch) != 10 {
		t.Fatalf("批量结果数量错误: %d", len(batch))
	}
	for i, res := range batch {
		if len(res.Items) != 9 {
			t.Errorf("第 %d 轮长度错误: %d", i, len(res.Items))
		}
	}

	// Stats 的并列处理必须稳定：取号码较小者。
	st := e.Stats([]int{5, 5, 3, 3, 1, 1})
	if st.TopNumber != 1 || st.TopCount != 2 {
		t.Errorf("并列时应当取号码较小者，实际 topNumber=%d topCount=%d", st.TopNumber, st.TopCount)
	}

	// 统计口径要和手算一致。
	st2 := e.Stats([]int{4, 4, 4, 1, 1, 8})
	if st2.SpecialCount != 3 {
		t.Errorf("特殊号码票数应为 3，实际 %d", st2.SpecialCount)
	}
	if st2.TopNumber != 4 || st2.TopCount != 3 {
		t.Errorf("最高票应为 4 号 3 票，实际 %d 号 %d 票", st2.TopNumber, st2.TopCount)
	}
}

// TestWeightsReflectExtraNumbers 确认号码池上界变化会摊薄非特殊号码的权重。
func TestWeightsReflectExtraNumbers(t *testing.T) {
	cfg := defaultCfg()
	cfg.MaxNumber = 20 // 1..20 去掉特殊号码 4，others 共 19 个

	e := newTestEngine(t, cfg, 1)
	w := e.Weights()
	if len(w) != 20 {
		t.Fatalf("权重表长度应为 20，实际 %d", len(w))
	}
	others := len(cfg.Others())
	if others != 19 {
		t.Fatalf("others 应为 19 个，实际 %d", others)
	}
	wantPer := 0.85 / float64(others)

	// 4 号（下标 3）保持 15%，其余号码均分 85%。
	if math.Abs(w[3]-0.15) > 1e-12 {
		t.Errorf("4 号权重应为 0.15，实际 %.12f", w[3])
	}
	if math.Abs(w[0]-wantPer) > 1e-12 {
		t.Errorf("1 号权重应为 %.12f，实际 %.12f", wantPer, w[0])
	}
	// 除 4 号外的号码权重必须完全一致。
	for i, n := range numberRangeOf(cfg) {
		if n == cfg.SpecialNumber {
			continue
		}
		if math.Abs(w[i]-wantPer) > 1e-12 {
			t.Errorf("号码 %d 权重应为 %.12f，实际 %.12f", n, wantPer, w[i])
		}
	}
}

// numberRangeOf 返回号码池的升序序列，与引擎的权重表顺序对应。
func numberRangeOf(cfg config.Config) []int {
	out := make([]int, 0, cfg.PoolSize())
	for n := cfg.MinNumber; n <= cfg.MaxNumber; n++ {
		out = append(out, n)
	}
	return out
}

// TestReconfigureRejectsPoolChange 号码池结构不允许在变体引擎里被改动。
func TestReconfigureRejectsPoolChange(t *testing.T) {
	e := newTestEngine(t, defaultCfg(), 1)

	bad := defaultCfg()
	bad.MaxNumber = 12
	if _, err := e.Reconfigure(bad); err == nil {
		t.Error("改变号码池上界应当被拒绝")
	}

	good := defaultCfg()
	good.DrawCount = 11
	good.SpecialProb = 0.2
	variant, err := e.Reconfigure(good)
	if err != nil {
		t.Fatalf("只改轮内次数与权重应当成功: %v", err)
	}
	if variant.Config().DrawCount != 11 {
		t.Errorf("变体配置未生效: %d", variant.Config().DrawCount)
	}
}

// TestPickCoversAllNumbers 抽样必须能取到号码池里的每一个号码，
// 避免累积概率表写错导致某些号码永远抽不到。
func TestPickCoversAllNumbers(t *testing.T) {
	e := newTestEngine(t, defaultCfg(), 6)
	seen := make(map[int]bool)
	for i := 0; i < 5000; i++ {
		seen[e.pick()] = true
	}
	for n := 1; n <= 8; n++ {
		if !seen[n] {
			t.Errorf("号码 %d 从未被抽到", n)
		}
	}
}

// BenchmarkDraw 衡量一轮抽奖的开销，含内部重抽。
func BenchmarkDraw(b *testing.B) {
	cfg := defaultCfg()
	e, err := NewWithSource(cfg, randsource.New())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := e.DrawChecked(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPick 衡量单次加权抽样的开销。
func BenchmarkPick(b *testing.B) {
	e, err := NewWithSource(defaultCfg(), randsource.New())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = e.pick()
	}
}
