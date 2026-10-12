// Package randsource 提供抽奖引擎使用的随机源。
//
// 设计取舍：
//   - 引擎只依赖 Source 接口，因此单元测试可以注入确定性伪随机源，
//     生产环境则注入 crypto/rand 实现，做到「可复现测试 + 不可预测线上」。
//   - 生产实现直接调用 crypto/rand，而不是用 crypto/rand 去播种 math/rand。
//     播种只发生一次，长期运行后 math/rand 的 607 个状态字可以被反推；
//     直接取样则每次都不依赖历史状态。
package randsource

import (
	crand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// Source 是引擎需要的最小随机能力集合。
type Source interface {
	// Float64 返回 [0, 1) 上的均匀分布值。
	Float64() float64
	// Intn 返回 [0, n) 上的均匀分布整数，n <= 0 时 panic。
	Intn(n int) int
	// Shuffle 原地打乱切片长度 n 的元素顺序。
	Shuffle(n int, swap func(i, j int))
}

// Crypto 是基于 crypto/rand 的 Source 实现，并发安全（无内部状态）。
type Crypto struct{}

// New 返回一个密码学安全的随机源。
func New() Crypto { return Crypto{} }

func (Crypto) bytes(b []byte) {
	if _, err := crand.Read(b); err != nil {
		// crypto/rand 在支持的平台上不会失败；真失败时没有安全降级选项，
		// 静默退回可预测随机数会让抽奖结果可被玩家操纵，因此直接 panic。
		panic(fmt.Sprintf("randsource: crypto/rand 读数失败: %v", err))
	}
}

// Float64 取 53 位随机尾数，覆盖 [0,1) 上全部可表示的 2^53 个值，
// 相对 math/rand 的 31 位做法精度更高，且各值等概率。
func (c Crypto) Float64() float64 {
	var b [8]byte
	c.bytes(b[:])
	// >>11 得到 53 位，再除以 2^53，结果严格小于 1。
	return float64(binary.LittleEndian.Uint64(b[:])>>11) / (1 << 53)
}

// Intn 使用「拒绝采样」消除取模偏置：
// limit 以下的整数个数恰好是 n 的整数倍，落在 [limit, 2^32) 的样本被丢弃重取。
func (c Crypto) Intn(n int) int {
	if n <= 0 {
		panic("randsource: Intn 的参数必须大于 0")
	}
	un := uint64(n)
	var b [4]byte
	limit := (uint64(1) << 32) - (uint64(1)<<32)%un
	for {
		c.bytes(b[:])
		v := uint64(binary.LittleEndian.Uint32(b[:]))
		if v < limit {
			return int(v % un)
		}
	}
}

// Shuffle 使用 Fisher-Yates 算法，每一步都从 crypto/rand 取随机下标。
func (c Crypto) Shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		j := c.Intn(i + 1)
		swap(i, j)
	}
}

// NewID 生成 128 位十六进制随机 ID，作为一次抽奖的幂等号 / 日志追踪号。
func NewID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("randsource: 生成 ID 失败: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// Seeded 是可复现的 Source，仅用于测试与本地演示。
// 它绝不用于生产抽奖：相同种子必然产生相同结果。
type Seeded struct {
	state uint64
}

// NewSeeded 用给定种子创建确定性随机源（SplitMix64）。
func NewSeeded(seed uint64) *Seeded { return &Seeded{state: seed} }

func (s *Seeded) next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Float64 实现 Source。
func (s *Seeded) Float64() float64 {
	return float64(s.next()>>11) / (1 << 53)
}

// Intn 实现 Source。
func (s *Seeded) Intn(n int) int {
	if n <= 0 {
		panic("randsource: Intn 的参数必须大于 0")
	}
	return int(s.next() % uint64(n))
}

// Shuffle 实现 Source。
func (s *Seeded) Shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		j := s.Intn(i + 1)
		swap(i, j)
	}
}
