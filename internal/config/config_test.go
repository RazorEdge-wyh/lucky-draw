package config

import (
	"strings"
	"testing"
)

// TestDefaultIsConsistent 默认配置必须能通过校验，
// 否则服务开箱即用就是坏的。
func TestDefaultIsConsistent(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("默认配置未通过校验: %v", err)
	}
	// 默认是**均匀随机**：SpecialNumber = 0 表示不启用任何有偏规则。
	if cfg.SpecialNumber != 0 {
		t.Errorf("默认应为均匀随机（SpecialNumber=0），实际 %d", cfg.SpecialNumber)
	}
	if cfg.Weighted() {
		t.Error("默认配置不应处于有偏模式")
	}
	if cfg.DrawCount != 9 {
		t.Errorf("默认一轮应为 9 次，实际 %d", cfg.DrawCount)
	}
	if cfg.MinNumber != 1 || cfg.MaxNumber != 8 {
		t.Errorf("默认号码池应为 1..8，实际 %d..%d", cfg.MinNumber, cfg.MaxNumber)
	}
}

// TestWeightedModeOptIn 有偏模式必须显式开启，且开启后 Weighted() 为真。
func TestWeightedModeOptIn(t *testing.T) {
	cfg := Default()
	cfg.SpecialNumber = 4
	if !cfg.Weighted() {
		t.Error("设置了池内特殊号码后应处于有偏模式")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("有偏模式配置应当合法: %v", err)
	}

	// 池外的号码应当被拒绝。
	bad := Default()
	bad.SpecialNumber = 99
	if err := bad.Validate(); err == nil {
		t.Error("池外的 SPECIAL_NUMBER 应当被拒绝")
	}

	// 负数同样应被拒绝。
	neg := Default()
	neg.SpecialNumber = -1
	if err := neg.Validate(); err == nil {
		t.Error("负数 SPECIAL_NUMBER 应当被拒绝")
	}
}

// TestValidateRejectsBadConfig 非法配置必须被拦住并给出可读原因。
func TestValidateRejectsBadConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string // 期望错误信息里出现的关键词
	}{
		{"上下界颠倒", func(c *Config) { c.MinNumber, c.MaxNumber = 9, 2 }, "MAX_NUMBER"},
		{"特殊号码越界", func(c *Config) { c.SpecialNumber = 99 }, "SPECIAL_NUMBER"},
		// 池子里只有特殊号码本身，特殊号码没越界，触发的是「池子太小」检查。
		{"号码池过小", func(c *Config) { c.MinNumber, c.MaxNumber = 4, 4 }, "至少"},
		{"概率为负", func(c *Config) { c.SpecialProb = -0.1 }, "SPECIAL_PROB"},
		{"概率大于一", func(c *Config) { c.SpecialProb = 1.5 }, "SPECIAL_PROB"},
		{"抽取次数为零", func(c *Config) { c.DrawCount = 0 }, "DRAW_COUNT"},
		{"重抽上限为零", func(c *Config) { c.MaxAttempts = 0 }, "MAX_ATTEMPTS"},
		{"端口为空", func(c *Config) { c.Port = "" }, "PORT"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("应当报错，实际通过校验: %+v", cfg)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息应包含 %q，实际 %q", tc.want, err.Error())
			}
		})
	}
}

// TestEnvOverrides 环境变量必须覆盖默认值。
func TestEnvOverrides(t *testing.T) {
	t.Setenv("DRAW_COUNT", "12")
	t.Setenv("SPECIAL_NUMBER", "6")
	t.Setenv("SPECIAL_PROB", "0.42")
	t.Setenv("MIN_NUMBER", "2")
	t.Setenv("MAX_NUMBER", "20")
	t.Setenv("MAX_ATTEMPTS", "77")
	t.Setenv("PORT", "9999")

	cfg := Load()
	if cfg.DrawCount != 12 || cfg.SpecialNumber != 6 || cfg.MaxNumber != 20 || cfg.MinNumber != 2 {
		t.Errorf("整数环境变量未生效: %+v", cfg)
	}
	if cfg.SpecialProb != 0.42 {
		t.Errorf("SPECIAL_PROB 未生效: %v", cfg.SpecialProb)
	}
	if cfg.MaxAttempts != 77 {
		t.Errorf("MAX_ATTEMPTS 未生效: %d", cfg.MaxAttempts)
	}
	if cfg.Port != "9999" {
		t.Errorf("PORT 未生效: %s", cfg.Port)
	}
}

// TestEnvInvalidFallsBackToDefault 非法环境变量不能让进程带着坏参数启动，
// 也不能 panic，应当静默退回默认值。
func TestEnvInvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("DRAW_COUNT", "not-a-number")
	t.Setenv("SPECIAL_PROB", "abc")
	t.Setenv("MAX_NUMBER", "")

	cfg := Load()
	def := Default()
	if cfg.DrawCount != def.DrawCount {
		t.Errorf("非法 DRAW_COUNT 应退回默认值 %d，实际 %d", def.DrawCount, cfg.DrawCount)
	}
	if cfg.SpecialProb != def.SpecialProb {
		t.Errorf("非法 SPECIAL_PROB 应退回默认值 %v，实际 %v", def.SpecialProb, cfg.SpecialProb)
	}
	if cfg.MaxNumber != def.MaxNumber {
		t.Errorf("空 MAX_NUMBER 应退回默认值 %d，实际 %d", def.MaxNumber, cfg.MaxNumber)
	}
}

// TestOthersAndPoolSize 均匀随机模式下 Others() 应返回完整号码池；
// 有偏模式下才排除特殊号码。
func TestOthersAndPoolSize(t *testing.T) {
	// 均匀随机：没有特殊号码，池子就是全部 8 个号码。
	cfg := Default()
	others := cfg.Others()
	if len(others) != 8 {
		t.Fatalf("均匀随机模式下 others 应为完整的 8 个号码，实际 %d: %v", len(others), others)
	}
	for i, n := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		if others[i] != n {
			t.Errorf("others[%d] 应为 %d，实际 %d（需保持升序）", i, n, others[i])
		}
	}
	if cfg.PoolSize() != 8 {
		t.Errorf("号码池大小应为 8，实际 %d", cfg.PoolSize())
	}

	// 有偏模式：排除特殊号码 4。
	biased := Default()
	biased.SpecialNumber = 4
	bOthers := biased.Others()
	if len(bOthers) != 7 {
		t.Fatalf("有偏模式下其他号码应有 7 个，实际 %d: %v", len(bOthers), bOthers)
	}
	for _, n := range bOthers {
		if n == 4 {
			t.Error("有偏模式的 others 不应包含特殊号码 4")
		}
	}
	for i, n := range []int{1, 2, 3, 5, 6, 7, 8} {
		if bOthers[i] != n {
			t.Errorf("有偏模式 others[%d] 应为 %d，实际 %d", i, n, bOthers[i])
		}
	}
}
