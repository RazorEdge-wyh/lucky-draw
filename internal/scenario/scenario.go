// Package scenario 定义抽奖「玩法模式」。
//
// 把模式独立出来的原因：同一个引擎在不同玩法下需要不同的概率参数，
// 而号码池结构（上下界、特殊号码）必须保持不变。
package scenario

import (
	"fmt"
	"strings"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
)

// Name 是模式标识。
type Name string

const (
	// ModeChallenging 是默认模式，即需求给定的参数：特殊号码 15%，整轮约束生效。
	ModeChallenging Name = "challenging"
	// ModeFair 是纯均匀模式：所有号码等概率，且不做任何整轮约束。
	// 适合真正需要等概率公平的场景；用来对比也能说明约束带来的偏差。
	ModeFair Name = "fair"
	// ModeHot 是特殊号码加权但不做整轮约束的模式：
	// 特殊号码更容易出现，但不保证是最高票，适合「中奖率高、票型不定」的玩法。
	ModeHot Name = "hot"
)

// Info 描述一个模式的元数据。
type Info struct {
	Name        Name
	Title       string
	Description string
	// Prob 是特殊号码的单次权重；Fair 模式取均匀值 1/PoolSize。
	Prob float64
	// Constrained 表示是否启用「特殊号码必须唯一最多」的整轮约束。
	Constrained bool
	// ProbIsExact 表示 Prob 是固定值而非按号码池推算。
	ProbIsExact bool
}

var registry = []Info{
	{
		Name:        ModeChallenging,
		Title:       "挑战模式（默认）",
		Description: "特殊号码单次权重 15%，并强制它成为本轮唯一最多票。结果票型固定，其余号码随机。",
		Prob:        0.15,
		Constrained: true,
		ProbIsExact: true,
	},
	{
		Name:        ModeFair,
		Title:       "等概率模式",
		Description: "所有号码完全等概率，不施加任何整轮约束，适合对公平性有硬要求的场景。",
		Prob:        0,
		Constrained: false,
		ProbIsExact: false,
	},
	{
		Name:        ModeHot,
		Title:       "加权模式",
		Description: "特殊号码权重 30%，但不保证它是最高票，票型每轮都可能不同。",
		Prob:        0.30,
		Constrained: false,
		ProbIsExact: true,
	},
}

// Default 返回默认模式。
func Default() Name { return ModeChallenging }

// All 返回全部模式的元数据副本。
func All() []Info {
	out := make([]Info, len(registry))
	copy(out, registry)
	return out
}

// Lookup 按名称查找模式，名称不区分大小写；空字符串视为默认模式。
func Lookup(name string) (Info, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return mustGet(Default()), nil
	}
	for _, s := range registry {
		if string(s.Name) == name {
			return s, nil
		}
	}
	return Info{}, fmt.Errorf("未知模式 %q，可选值：%s", name, names())
}

func mustGet(n Name) Info {
	for _, s := range registry {
		if s.Name == n {
			return s
		}
	}
	panic("scenario: 注册表中缺少 " + string(n))
}

func names() string {
	out := make([]string, 0, len(registry))
	for _, s := range registry {
		out = append(out, string(s.Name))
	}
	return strings.Join(out, ", ")
}

// Apply 把模式应用到基础配置上，返回该模式实际使用的配置。
//
// 注意：等概率模式把 SpecialProb 设为 1/PoolSize，此时引擎的加权逻辑
// 退化成均匀抽样，但号码池结构与特殊号码的定义仍然有效。
func (i Info) Apply(base config.Config) config.Config {
	cfg := base
	if i.ProbIsExact {
		cfg.SpecialProb = i.Prob
	} else {
		cfg.SpecialProb = 1.0 / float64(cfg.PoolSize())
	}
	cfg.Constrained = i.Constrained
	return cfg
}
