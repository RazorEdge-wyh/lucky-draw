package engine

import (
	"errors"
	"math"
	"testing"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/randsource"
)

// ===========================================================================
// 这里的测试守护的是**备份下来的原始有偏算法**。
//
// 现在默认已经改成均匀随机（config.Default() 的 SpecialNumber 为 0），
// 但这条老路径必须依然可用、结果必须依然符合当初的约定：
// 一轮九次，特殊号码一定出现且是唯一最高票。
//
// 只要这些测试还是绿的，就说明「备份」是活的、可用的，而不是一段死代码。
// ===========================================================================

// TestBiasedGeneratorStillGuaranteesSpecial 原始算法在 200 轮里都必须满足约束。
func TestBiasedGeneratorStillGuaranteesSpecial(t *testing.T) {
	cfg := biasedCfg()
	if !cfg.Weighted() {
		t.Fatal("biasedCfg 应当处于有偏模式")
	}

	g, err := NewBiasedGeneratorWithSource(cfg, randsource.NewSeeded(20261012))
	if err != nil {
		t.Fatalf("构造有偏生成器失败: %v", err)
	}

	for i := 0; i < 200; i++ {
		res := g.Draw()
		if res.Fallback {
			t.Fatalf("第 %d 轮触发了兜底（默认参数下不该发生）", i)
		}
		items := res.Items
		if len(items) != 9 {
			t.Fatalf("第 %d 轮长度应为 9，实际 %d", i, len(items))
		}

		counts := map[int]int{}
		for _, n := range items {
			counts[n]++
		}
		if counts[cfg.SpecialNumber] == 0 {
			t.Fatalf("第 %d 轮没有命中 %d：%v", i, cfg.SpecialNumber, items)
		}
		for n, c := range counts {
			if n != cfg.SpecialNumber && c >= counts[cfg.SpecialNumber] {
				t.Fatalf("第 %d 轮 %d 不是唯一最高票：%v", i, cfg.SpecialNumber, items)
			}
		}
	}
}

// TestBiasedGeneratorResultDistribution 备份算法的结果分布必须保持原样：
// 4 的占比约 37%，其他号码各约 9%（这是「整轮必中」带来的条件化抬高）。
//
// 这个数字以前写在 README 里，现在它是「老算法没被改坏」的指纹。
func TestBiasedGeneratorResultDistribution(t *testing.T) {
	cfg := biasedCfg()
	g, err := NewBiasedGeneratorWithSource(cfg, randsource.NewSeeded(4242))
	if err != nil {
		t.Fatalf("构造有偏生成器失败: %v", err)
	}

	const rounds = 30000
	dist := map[int]int{}
	total := 0
	for i := 0; i < rounds; i++ {
		for _, n := range g.DrawSequence() {
			dist[n]++
			total++
		}
	}

	share4 := float64(dist[cfg.SpecialNumber]) / float64(total)
	if math.Abs(share4-0.3715) > 0.02 {
		t.Errorf("备份算法里 %d 的占比应为约 37.15%%，实际 %.2f%%（算法可能被改坏了）",
			cfg.SpecialNumber, share4*100)
	}

	for _, n := range g.others {
		share := float64(dist[n]) / float64(total)
		if math.Abs(share-0.0897) > 0.01 {
			t.Errorf("备份算法里号码 %d 的占比应约 8.97%%，实际 %.2f%%", n, share*100)
		}
	}
}

// TestBiasedGeneratorWeights 权重表必须还是 15% / 12.14%。
func TestBiasedGeneratorWeights(t *testing.T) {
	g, err := NewBiasedGeneratorWithSource(biasedCfg(), randsource.NewSeeded(1))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	w := g.Weights()
	if len(w) != 8 {
		t.Fatalf("权重表长度应为 8，实际 %d", len(w))
	}
	if math.Abs(w[3]-0.15) > 1e-12 {
		t.Errorf("4 号权重应为 0.15，实际 %.12f", w[3])
	}
	if math.Abs(w[0]-(0.85/7)) > 1e-12 {
		t.Errorf("1 号权重应为 %.12f，实际 %.12f", 0.85/7, w[0])
	}
}

// TestBiasedGeneratorRequiresWeightedConfig 均匀随机配置不能构造有偏生成器：
// 否则调用方会以为自己在跑老算法，实际上并没有约束。
func TestBiasedGeneratorRequiresWeightedConfig(t *testing.T) {
	if _, err := NewBiasedGenerator(uniformCfg()); !errors.Is(err, ErrNotWeighted) {
		t.Errorf("均匀随机配置应返回 ErrNotWeighted，实际 %v", err)
	}
}

// TestBiasedGeneratorFallback 强制走兜底分支：权重为 0 + 重抽上限 1。
func TestBiasedGeneratorFallback(t *testing.T) {
	cfg := biasedCfg()
	cfg.SpecialProb = 0 // 特殊号码永远抽不到
	cfg.MaxAttempts = 1

	g, err := NewBiasedGeneratorWithSource(cfg, randsource.NewSeeded(7))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	for i := 0; i < 50; i++ {
		res := g.Draw()
		if !res.Fallback {
			t.Fatalf("第 %d 轮应标记为兜底", i)
		}
		counts := map[int]int{}
		for _, n := range res.Items {
			counts[n]++
		}
		if counts[4] == 0 {
			t.Fatalf("兜底结果里 4 必须出现: %v", res.Items)
		}
		for n, c := range counts {
			if n != 4 && c >= counts[4] {
				t.Fatalf("兜底结果里 4 必须是唯一最高票: %v", res.Items)
			}
		}
		// 最小可行 k=2（9 个位置、7 个其他号码）
		if counts[4] != 2 {
			t.Fatalf("兜底应取最小可行 k=2，实际 4 出现 %d 次: %v", counts[4], res.Items)
		}
	}
}

// TestBiasedAndUniformDiffer 两种算法必须真的不同：
// 同样的种子下，有偏模式每轮必出 4，均匀模式则经常不出 4。
//
// 这是「默认确实改成真随机了」的直接证据。
func TestBiasedAndUniformDiffer(t *testing.T) {
	const rounds = 200

	// 均匀随机：统计有多少轮完全没有出现 4
	uniform := newTestEngine(t, uniformCfg(), 999)
	uniformMissing := 0
	for i := 0; i < rounds; i++ {
		seq := uniform.DrawSequence()
		missing := true
		for _, n := range seq {
			if n == 4 {
				missing = false
				break
			}
		}
		if missing {
			uniformMissing++
		}
	}

	// 有偏：一轮都不会缺 4
	biased := newTestEngine(t, biasedCfg(), 999)
	biasedMissing := 0
	for i := 0; i < rounds; i++ {
		seq := biased.DrawSequence()
		missing := true
		for _, n := range seq {
			if n == 4 {
				missing = false
				break
			}
		}
		if missing {
			biasedMissing++
		}
	}

	t.Logf("均匀随机下 %d/%d 轮完全没有出现 4；有偏模式下 %d/%d 轮缺 4",
		uniformMissing, rounds, biasedMissing, rounds)

	if biasedMissing != 0 {
		t.Errorf("有偏模式不该有任何一轮缺少 4，实际 %d 轮", biasedMissing)
	}
	// 均匀随机下「9 次全不出 4」的概率约 (7/8)^9 ≈ 30%，200 轮约有 60 轮
	if uniformMissing < rounds/5 {
		t.Errorf("均匀随机下缺少 4 的轮数应约 30%%，实际只有 %d/%d 轮", uniformMissing, rounds)
	}
}

// TestUniformWeightsAreEqual 默认模式的权重必须完全相等。
func TestUniformWeightsAreEqual(t *testing.T) {
	e := newTestEngine(t, uniformCfg(), 1)
	w := e.Weights()
	if len(w) != 8 {
		t.Fatalf("权重表长度应为 8，实际 %d", len(w))
	}
	for i, v := range w {
		if math.Abs(v-0.125) > 1e-12 {
			t.Errorf("均匀随机下第 %d 个号码权重应为 0.125，实际 %.12f", i, v)
		}
	}
	if e.Weighted() {
		t.Error("默认配置不应处于有偏模式")
	}
}

// TestUniformSpecialCountIsZero 均匀随机下 Stats.SpecialCount 必须恒为 0，
// 避免调用方误以为存在「保底号码」。
func TestUniformSpecialCountIsZero(t *testing.T) {
	e := newTestEngine(t, uniformCfg(), 5)
	for i := 0; i < 50; i++ {
		st := e.Stats(e.DrawSequence())
		if st.SpecialCount != 0 {
			t.Fatalf("均匀随机下 SpecialCount 应为 0，实际 %d", st.SpecialCount)
		}
	}
}

// TestFirstNumberIsFixed 首位固定规则必须稳定：每一轮的第一个号码都是号码池上界。
//
// 这条规则是刻意设计的（不是随机结果），因此单独用测试钉住，防止被无意改掉。
func TestFirstNumberIsFixed(t *testing.T) {
	cfg := uniformCfg()
	e := newTestEngine(t, cfg, 11)

	for i := 0; i < 300; i++ {
		seq := e.DrawSequence()
		if len(seq) != cfg.DrawCount {
			t.Fatalf("第 %d 轮长度应为 %d，实际 %d", i, cfg.DrawCount, len(seq))
		}
		if seq[0] != cfg.MaxNumber {
			t.Fatalf("第 %d 轮第一个号码应为 %d，实际 %d（序列 %v）",
				i, cfg.MaxNumber, seq[0], seq)
		}
	}

	// 引擎必须如实报告这条规则，供接口层转达给调用方。
	if n, fixed := e.FirstNumberFixed(); !fixed || n != cfg.MaxNumber {
		t.Errorf("FirstNumberFixed 应返回 (%d, true)，实际 (%d, %t)", cfg.MaxNumber, n, fixed)
	}
}

// TestFirstNumberFixedFollowsPoolSize 首位固定的是「号码池上界」，随配置变化，
// 不是写死的 8。这样改 MAX_NUMBER 时行为是自洽的。
func TestFirstNumberFixedFollowsPoolSize(t *testing.T) {
	for _, maxNumber := range []int{6, 8, 12, 20} {
		cfg := uniformCfg()
		cfg.MaxNumber = maxNumber
		e := newTestEngine(t, cfg, 3)

		if n, fixed := e.FirstNumberFixed(); !fixed || n != maxNumber {
			t.Errorf("MAX_NUMBER=%d 时 FirstNumberFixed 应返回 (%d, true)，实际 (%d, %t)",
				maxNumber, maxNumber, n, fixed)
		}
		for i := 0; i < 50; i++ {
			seq := e.DrawSequence()
			if seq[0] != maxNumber {
				t.Fatalf("MAX_NUMBER=%d 时第 %d 轮首位应为 %d，实际 %d",
					maxNumber, i, maxNumber, seq[0])
			}
			// 其余位置必须仍然落在号码池内。
			for _, n := range seq {
				if n < cfg.MinNumber || n > maxNumber {
					t.Fatalf("号码越界: %d（池 %d..%d）", n, cfg.MinNumber, maxNumber)
				}
			}
		}
	}
}

// TestUniformDrawDistribution 第 2..9 位的分布校验：每个号码占比约 1/8。
//
// 首位被固定为 8，因此它天然偏离 1/8（约 1/9 + 8/9·1/8 ≈ 22%），
// 这里只统计其余位置，避免把「刻意固定」误判成「偏向」。
func TestUniformDrawDistribution(t *testing.T) {
	e := newTestEngine(t, uniformCfg(), 2026)

	const rounds = 40000
	dist := map[int]int{}
	restDraws := 0

	for i := 0; i < rounds; i++ {
		seq := e.DrawSequence()
		for pos, n := range seq {
			if pos == 0 {
				continue // 首位固定，不参与均匀性统计
			}
			dist[n]++
			restDraws++
		}
	}

	want := 1.0 / 8.0
	for n := 1; n <= 8; n++ {
		share := float64(dist[n]) / float64(restDraws)
		// 3.2e5 次抽样的二项标准差约 0.00055，取 0.004 容忍度
		if math.Abs(share-want) > 0.004 {
			t.Errorf("号码 %d 在其余位置的占比 %.5f，期望 %.5f（偏离过大）", n, share, want)
		}
	}
}

// TestConfigDefaultIsUniformContract 把「默认必须是真随机」这条契约固定下来。
func TestConfigDefaultIsUniformContract(t *testing.T) {
	cfg := config.Default()
	if cfg.Weighted() {
		t.Error("config.Default() 必须处于均匀随机模式（SpecialNumber = 0）")
	}
	if cfg.SpecialNumber != 0 {
		t.Errorf("默认 SpecialNumber 应为 0，实际 %d", cfg.SpecialNumber)
	}
}
