package randsource

import (
	"strings"
	"sync"
	"testing"
)

// TestCryptoFloat64Range Float64 必须严格落在 [0,1) —— 这是加权抽样累积表
// 能正确工作的前提：越界值会让抽样落到表外。
func TestCryptoFloat64Range(t *testing.T) {
	src := New()
	minSeen, maxSeen := 1.0, 0.0
	for i := 0; i < 200000; i++ {
		f := src.Float64()
		if f < 0 || f >= 1 {
			t.Fatalf("第 %d 次取值越界: %v", i, f)
		}
		if f < minSeen {
			minSeen = f
		}
		if f > maxSeen {
			maxSeen = f
		}
	}
	t.Logf("20 万次取值的范围：[%.6f, %.6f]", minSeen, maxSeen)

	// 覆盖度检查：把 [0,1) 切成 10 个桶，每桶都应有样本，且占比接近 1/10。
	const buckets = 10
	const n = 200000
	hist := make([]int, buckets)
	for i := 0; i < n; i++ {
		hist[int(src.Float64()*buckets)]++
	}
	for i, c := range hist {
		share := float64(c) / n
		if share < 0.09 || share > 0.11 {
			t.Errorf("第 %d 个桶占比 %.4f，期望约 0.1（分布可能不均匀）", i, share)
		}
	}
}

// TestCryptoIntnUniform Intn 必须无偏且不越界。
//
// 取模偏置的典型症状是某些取值明显偏多，因此这里对每个取值的频率设了较紧的区间。
func TestCryptoIntnUniform(t *testing.T) {
	src := New()
	const n = 7
	const rounds = 210000
	hist := make([]int, n)
	for i := 0; i < rounds; i++ {
		v := src.Intn(n)
		if v < 0 || v >= n {
			t.Fatalf("Intn(%d) 越界: %d", n, v)
		}
		hist[v]++
	}
	for v, c := range hist {
		share := float64(c) / rounds
		want := 1.0 / n
		if diff := share - want; diff > 0.01 || diff < -0.01 {
			t.Errorf("Intn(%d) 取值 %d 占比 %.4f，期望 %.4f（可能存在取模偏置）", n, v, share, want)
		}
	}
}

// TestCryptoIntnPanicsOnNonPositive 非法参数必须 panic，而不是返回垃圾值。
func TestCryptoIntnPanicsOnNonPositive(t *testing.T) {
	src := New()
	for _, n := range []int{0, -1} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Intn(%d) 应当 panic", n)
				}
			}()
			_ = src.Intn(n)
		}()
	}
}

// TestCryptoShuffleShuffles 洗牌必须真的打乱，且不能丢元素。
func TestCryptoShuffleShuffles(t *testing.T) {
	src := New()
	const n = 20
	const attempts = 200

	sameAsOriginal := 0
	for a := 0; a < attempts; a++ {
		s := make([]int, n)
		for i := range s {
			s[i] = i
		}
		src.Shuffle(n, func(i, j int) { s[i], s[j] = s[j], s[i] })

		// 元素集合必须完全一致。
		seen := make([]bool, n)
		for _, v := range s {
			if v < 0 || v >= n {
				t.Fatalf("洗牌后出现非法元素: %d", v)
			}
			if seen[v] {
				t.Fatalf("洗牌后元素重复: %d", v)
			}
			seen[v] = true
		}

		// 统计有多少次「完全没变」。
		identical := true
		for i, v := range s {
			if v != i {
				identical = false
				break
			}
		}
		if identical {
			sameAsOriginal++
		}
	}
	// 20! 种排列里恰好还是原序的概率极低，200 次里出现多于 1 次说明洗牌没生效。
	if sameAsOriginal > 1 {
		t.Errorf("%d/%d 次洗牌后顺序完全不变，洗牌可能没生效", sameAsOriginal, attempts)
	}
}

// TestCryptoIsConcurrencySafe 生产随机源必须能被多协程共享：它没有任何内部状态。
// 这个测试在 -race 下才有意义（CI 会开）。
func TestCryptoIsConcurrencySafe(t *testing.T) {
	src := New()
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				if f := src.Float64(); f < 0 || f >= 1 {
					t.Errorf("并发下 Float64 越界: %v", f)
					return
				}
				_ = src.Intn(10)
			}
		}()
	}
	wg.Wait()
}

// TestNewIDFormat NewID 必须是 32 位十六进制且不重复。
func TestNewIDFormat(t *testing.T) {
	const n = 20000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := NewID()
		if len(id) != 32 {
			t.Fatalf("ID 长度应为 32，实际 %d：%q", len(id), id)
		}
		if strings.Trim(id, "0123456789abcdef") != "" {
			t.Fatalf("ID 只能包含小写十六进制字符：%q", id)
		}
		if seen[id] {
			t.Fatalf("ID 重复: %q", id)
		}
		seen[id] = true
	}
}

// TestSeededIsReproducible 相同种子的确定性源必须产生完全相同的序列。
func TestSeededIsReproducible(t *testing.T) {
	a := NewSeeded(12345)
	b := NewSeeded(12345)
	for i := 0; i < 1000; i++ {
		if x, y := a.Float64(), b.Float64(); x != y {
			t.Fatalf("第 %d 次取值不同: %v vs %v", i, x, y)
		}
	}

	// 不同种子必须产生不同序列。
	c := NewSeeded(54321)
	d := NewSeeded(12345)
	identical := 0
	for i := 0; i < 100; i++ {
		if c.Float64() == d.Float64() {
			identical++
		}
	}
	if identical > 5 {
		t.Errorf("不同种子下有 %d/100 次取值相同，随机源可能失效", identical)
	}
}

// TestSeededIntnRange Seeded 的 Intn 也必须不越界。
func TestSeededIntnRange(t *testing.T) {
	src := NewSeeded(99)
	const n = 5
	seen := make(map[int]bool, n)
	for i := 0; i < 10000; i++ {
		v := src.Intn(n)
		if v < 0 || v >= n {
			t.Fatalf("Intn(%d) 越界: %d", n, v)
		}
		seen[v] = true
	}
	if len(seen) != n {
		t.Errorf("Intn(%d) 未覆盖全部取值: %v", n, seen)
	}
}

func BenchmarkCryptoFloat64(b *testing.B) {
	src := New()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = src.Float64()
	}
}

func BenchmarkSeededFloat64(b *testing.B) {
	src := NewSeeded(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = src.Float64()
	}
}
