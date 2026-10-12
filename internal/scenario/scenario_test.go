package scenario

import (
	"strings"
	"testing"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
)

// TestLookupDefaults 空名称应落到默认模式（挑战模式）。
func TestLookupDefaults(t *testing.T) {
	for _, name := range []string{"", "  ", "CHALLENGING", "Challenging"} {
		got, err := Lookup(name)
		if err != nil {
			t.Fatalf("Lookup(%q) 不应报错: %v", name, err)
		}
		if got.Name != ModeChallenging {
			t.Errorf("Lookup(%q) 应返回挑战模式，实际 %s", name, got.Name)
		}
	}
}

// TestLookupUnknown 未知模式必须报错，并提示可选项。
func TestLookupUnknown(t *testing.T) {
	_, err := Lookup("nope")
	if err == nil {
		t.Fatal("未知模式应当报错")
	}
	for _, want := range []string{"nope", "challenging", "fair", "hot"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息应包含 %q，实际 %q", want, err.Error())
		}
	}
}

// TestApplySetsProbAndConstraint 模式应用后权重与约束开关都要正确。
func TestApplySetsProbAndConstraint(t *testing.T) {
	base := config.Default()

	challenging, _ := Lookup("challenging")
	got := challenging.Apply(base)
	if got.SpecialProb != 0.15 {
		t.Errorf("挑战模式权重应为 0.15，实际 %v", got.SpecialProb)
	}
	if !got.Constrained {
		t.Error("挑战模式应启用整轮约束")
	}

	hot, _ := Lookup("hot")
	got = hot.Apply(base)
	if got.SpecialProb != 0.30 {
		t.Errorf("加权模式权重应为 0.30，实际 %v", got.SpecialProb)
	}
	if got.Constrained {
		t.Error("加权模式不应启用整轮约束")
	}

	fair, _ := Lookup("fair")
	got = fair.Apply(base)
	// 8 个号码等概率 → 每个 12.5%。
	if diff := got.SpecialProb - 0.125; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("等概率模式权重应为 0.125，实际 %v", got.SpecialProb)
	}
	if got.Constrained {
		t.Error("等概率模式不应启用整轮约束")
	}
	if err := got.Validate(); err != nil {
		t.Errorf("应用模式后配置应仍然合法: %v", err)
	}
}

// TestApplyDoesNotMutateBase Apply 必须返回值语义，不能改到入参。
func TestApplyDoesNotMutateBase(t *testing.T) {
	base := config.Default()
	hot, _ := Lookup("hot")
	_ = hot.Apply(base)

	if base.SpecialProb != 0.15 {
		t.Errorf("Apply 改动了入参: %v", base.SpecialProb)
	}
	if !base.Constrained {
		t.Error("Apply 改动了入参的约束开关")
	}
}

// TestFairProbAdaptsToPoolSize 等概率模式的权重应随号码池大小变化。
func TestFairProbAdaptsToPoolSize(t *testing.T) {
	fair, _ := Lookup("fair")

	base := config.Default()
	base.MinNumber, base.MaxNumber = 1, 20 // 20 个号码
	got := fair.Apply(base)
	if diff := got.SpecialProb - 0.05; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("20 个号码时等概率权重应为 0.05，实际 %v", got.SpecialProb)
	}
}

// TestAllReturnsCopy All 返回的切片被改动时不应污染注册表。
func TestAllReturnsCopy(t *testing.T) {
	first := All()
	if len(first) == 0 {
		t.Fatal("注册表不应为空")
	}
	first[0].Title = "被改坏了"

	if All()[0].Title == "被改坏了" {
		t.Error("All 返回的是内部切片，外部改动污染了注册表")
	}
}
