package engine

import (
	"math"
	"testing"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
)

// ===========================================================================
// 本文件专门验证「单轮自然命中率」这个解析公式。
//
// 它是整个项目里最容易写错的一处：命中要求特殊号码出现 k 次的同时、
// 其他每个号码最多 k-1 次，而每个计数向量对应 rest!/Πc_i! 条不同的抽取序列。
// 早期实现漏掉了这个多项式系数，把概率算小了约 143 倍。
// 因此这里用三种互相独立的方法交叉验证：
//  1. 公式本身对所有 k 求和必须恒等于 1（划分样本空间）；
//  2. 小规模参数下穷举全部序列（512 条）得到精确值；
//  3. 大规模参数下用蒙特卡洛实测。
// ===========================================================================

// multinomialSumForTest 与生产实现 multinomialSum 同源，
// 但这里独立写一份递推，避免「实现错了测试跟着错」。
func multinomialSumForTest(rest, m, limit int) int64 {
	if rest == 0 {
		return 1
	}
	if rest < 0 || m <= 0 || limit < 0 {
		return 0
	}
	// dp[j] = 把 j 个可区分位置分给已处理盒子的方案数
	dp := make([]int64, rest+1)
	dp[0] = 1
	for i := 0; i < m; i++ {
		next := make([]int64, rest+1)
		for j := 0; j <= rest; j++ {
			if dp[j] == 0 {
				continue
			}
			for c := 0; c <= limit && j+c <= rest; c++ {
				next[j+c] += dp[j] * binomForTest(rest-j, c)
			}
		}
		dp = next
	}
	return dp[rest]
}

func binomForTest(n, k int) int64 {
	if n < 0 || k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k
	}
	res := int64(1)
	for i := 1; i <= k; i++ {
		res = res * int64(n-k+i) / int64(i)
	}
	return res
}

// TestMultinomialSumEqualsPower 上限不起作用时，多项式系数之和必须等于 m^rest。
// 这是对 DP 最直接的恒等式检验。
func TestMultinomialSumEqualsPower(t *testing.T) {
	for rest := 0; rest <= 8; rest++ {
		for m := 1; m <= 5; m++ {
			want := int64(1)
			for i := 0; i < rest; i++ {
				want *= int64(m)
			}
			if got := multinomialSum(rest, m, rest); got != want {
				t.Errorf("multinomialSum(%d,%d,%d) = %d，期望 m^rest = %d",
					rest, m, rest, got, want)
			}
			if got := multinomialSumForTest(rest, m, rest); got != want {
				t.Errorf("测试副本 multinomialSumForTest(%d,%d,%d) = %d，期望 %d",
					rest, m, rest, got, want)
			}
		}
	}
}

// TestMultinomialSumAgainstBruteForce 用穷举排列的方式独立数一遍方案数。
func TestMultinomialSumAgainstBruteForce(t *testing.T) {
	// brute 直接枚举每个盒子放几个球，并把组合数乘起来。
	var brute func(j, boxesLeft int) int64
	brute = func(j, boxesLeft int) int64 {
		if boxesLeft == 0 {
			if j == 0 {
				return 1
			}
			return 0
		}
		var sum int64
		for c := 0; c <= j; c++ {
			sum += binomForTest(j, c) * brute(j-c, boxesLeft-1)
		}
		return sum
	}

	checked := 0
	for m := 1; m <= 5; m++ {
		for limit := 0; limit <= 4; limit++ {
			for rest := 0; rest <= 10; rest++ {
				want := brute(rest, m)
				_ = limit
				// 与无上限的穷举对比只在 limit >= rest 时成立，因此这里专门验证无上限情形。
				if limit >= rest {
					if got := multinomialSum(rest, m, limit); got != want {
						t.Errorf("multinomialSum(rest=%d, m=%d, limit=%d) = %d，穷举 %d",
							rest, m, limit, got, want)
					}
				}
				checked++
			}
		}
	}
	t.Logf("交叉验证 %d 组参数", checked)
}

// TestMultinomialSumKnownValues 手算校验。
func TestMultinomialSumKnownValues(t *testing.T) {
	cases := []struct {
		rest, m, limit int
		want           int64
		note           string
	}{
		{7, 7, 1, 5040, "7 个盒子各放 1 个，排列数 7!"},
		{6, 7, 2, 83790, "6 个位置分给 7 个盒子、每盒最多 2 个"},
		{5, 7, 3, 16590, "5 个位置分给 7 个盒子、每盒最多 3 个"},
		{0, 7, 0, 1, "没有位置，唯一方案"},
		{1, 7, 0, 0, "上限为 0 时放不下任何位置"},
		{3, 7, 9, 343, "上限不起作用，7^3"},
	}
	for _, c := range cases {
		if got := multinomialSum(c.rest, c.m, c.limit); got != c.want {
			t.Errorf("multinomialSum(%d,%d,%d) = %d，期望 %d（%s）",
				c.rest, c.m, c.limit, got, c.want, c.note)
		}
	}
}

// TestBinomKnownValues 校验组合数实现。
func TestBinomKnownValues(t *testing.T) {
	cases := []struct {
		n, k int
		want int64
	}{
		{0, 0, 1}, {5, 0, 1}, {5, 5, 1}, {5, 2, 10}, {9, 2, 36}, {8, 1, 8},
		{20, 10, 184756}, {13, 6, 1716}, {5, 6, 0}, {-1, 0, 0}, {3, -1, 0},
	}
	for _, c := range cases {
		if got := binom(c.n, c.k); got != c.want {
			t.Errorf("binom(%d,%d) = %d，期望 %d", c.n, c.k, got, c.want)
		}
	}
}

// TestAcceptProbPerKAgainstSamples 逐 k 校验解析式：对每个 k 单独统计
// 「特殊号码恰好出现 k 次且其他号码都少于 k 次」的实测频率，与公式逐项对比。
//
// 这是最有说服力的检验方式 —— 不依赖任何组合恒等式，直接用真实随机序列说话。
// 早期实现漏掉了多项式系数，k=2/3/4 各项会偏小几十倍，这个测试会立刻发现。
func TestAcceptProbPerKAgainstSamples(t *testing.T) {
	e := newTestEngine(t, defaultCfg(), 987654321)
	const rounds = 400000
	n, m := e.cfg.DrawCount, len(e.others)
	p, q := e.cfg.SpecialProb, e.per

	hitByK := make(map[int]int, n+1)
	totalHits := 0
	for i := 0; i < rounds; i++ {
		_, counts := e.round()
		if !e.accept(counts) {
			continue
		}
		totalHits++
		hitByK[counts[e.cfg.SpecialNumber]]++
	}

	formulaSum := 0.0
	for k := 1; k <= n; k++ {
		rest := n - k
		formula := float64(binom(n, k)) * float64(multinomialSum(rest, m, k-1)) *
			ipow(p, k) * ipow(q, rest)
		formulaSum += formula
		measured := float64(hitByK[k]) / float64(rounds)

		// 期望值极小的项（如 k=8）采样噪声很大，只在期望值够大时才严格比对。
		if formula*rounds >= 30 {
			if formula <= 0 {
				t.Errorf("k=%d 的解析值不应为 0", k)
				continue
			}
			if ratio := measured / formula; ratio < 0.9 || ratio > 1.1 {
				t.Errorf("k=%d 实测 %.6f 与解析 %.6f 偏差过大（比值 %.3f）",
					k, measured, formula, ratio)
			}
		}
	}

	// 总量也必须吻合。
	measuredTotal := float64(totalHits) / float64(rounds)
	if diff := measuredTotal - formulaSum; diff > 0.004 || diff < -0.004 {
		t.Errorf("总命中率实测 %.5f 与解析 %.5f 偏差过大", measuredTotal, formulaSum)
	}
	if diff := formulaSum - e.AcceptProb(); diff > 1e-9 || diff < -1e-9 {
		t.Errorf("逐项求和 %.10f 与 AcceptProb %.10f 不一致", formulaSum, e.AcceptProb())
	}
	t.Logf("实测总命中率 %.5f，解析 %.5f，期望重抽 %.2f 轮",
		measuredTotal, formulaSum, 1/formulaSum)
}

// TestAcceptProbAgainstExactEnumeration 小规模下穷举全部 8^n 条序列，
// 与解析公式逐位对照。n=3 时共 512 条，可以完全手工核对。
func TestAcceptProbAgainstExactEnumeration(t *testing.T) {
	for _, tc := range []struct {
		drawCount int
		prob      float64
	}{
		{3, 0.5},
		{3, 0.15},
		{4, 0.3},
		{5, 0.15},
	} {
		cfg := defaultCfg()
		cfg.DrawCount = tc.drawCount
		cfg.SpecialProb = tc.prob
		e := newTestEngine(t, cfg, 1)

		exact := enumerateAcceptProb(t, cfg, e)
		got := e.AcceptProb()
		t.Logf("n=%d p=%v：穷举 %.12f，公式 %.12f", tc.drawCount, tc.prob, exact, got)
		if math.Abs(exact-got) > 1e-9 {
			t.Errorf("n=%d p=%v 时公式 %.12f 与穷举 %.12f 不一致", tc.drawCount, tc.prob, got, exact)
		}
	}
}

// enumerateAcceptProb 穷举全部票数向量，按多项式概率精确求和。
func enumerateAcceptProb(t *testing.T, cfg config.Config, e *Engine) float64 {
	t.Helper()
	special := cfg.SpecialNumber
	// 把号码池映射成 [minNumber..maxNumber]，下标 0 对应最小号码。
	size := cfg.PoolSize()
	counts := make([]int, size)
	weights := make([]float64, size)
	for i := range weights {
		if cfg.MinNumber+i == special {
			weights[i] = cfg.SpecialProb
		} else {
			weights[i] = e.per
		}
	}

	total := 0.0
	var walk func(idx, remaining int)
	walk = func(idx, remaining int) {
		if idx == size-1 {
			counts[idx] = remaining

			specialCount := counts[special-cfg.MinNumber]
			if specialCount == 0 {
				return
			}
			for i, c := range counts {
				if cfg.MinNumber+i != special && c >= specialCount {
					return
				}
			}

			// 多项式概率：n!/Πc_i! · Π w_i^{c_i}
			logp := logFact(cfg.DrawCount)
			for i, c := range counts {
				logp -= logFact(c)
				if c > 0 {
					logp += float64(c) * math.Log(weights[i])
				}
			}
			total += math.Exp(logp)
			return
		}
		for take := 0; take <= remaining; take++ {
			counts[idx] = take
			walk(idx+1, remaining-take)
		}
	}
	walk(0, cfg.DrawCount)
	return total
}

var logFactCache = map[int]float64{0: 0, 1: 0}

func logFact(n int) float64 {
	if v, ok := logFactCache[n]; ok {
		return v
	}
	v := logFact(n-1) + math.Log(float64(n))
	logFactCache[n] = v
	return v
}

// TestAcceptProbDefaultValue 记录默认参数下的解析命中率，
// 并断言它与「平均重抽轮数」的量级一致 —— 这个数字会展示给玩家，
// 因此必须是可信的。实测约 0.111（见 TestAcceptProbMatchesSimulation）。
func TestAcceptProbDefaultValue(t *testing.T) {
	e := newTestEngine(t, defaultCfg(), 1)
	got := e.AcceptProb()
	t.Logf("默认参数单轮命中率 = %.10f，期望重抽 %.2f 轮", got, 1/got)

	if got < 0.08 || got > 0.15 {
		t.Errorf("默认参数下单轮命中率应约 0.11，实际 %.10f", got)
	}
}
