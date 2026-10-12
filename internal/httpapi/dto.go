package httpapi

import (
	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/engine"
	"github.com/RazorEdge-wyh/lucky-draw/internal/scenario"
)

// 本文件集中定义对外 JSON 契约。
// 所有时间戳均为毫秒级 Unix 时间，所有比率均为 0..1 的小数。

// errorResponse 是所有失败响应的统一结构。
type errorResponse struct {
	Error  string `json:"error"`
	Status int    `json:"status"`
}

// configJSON 是生效配置的对外视图。
type configJSON struct {
	DrawCount     int     `json:"drawCount"`
	SpecialNumber int     `json:"specialNumber"`
	SpecialProb   float64 `json:"specialProb"`
	MinNumber     int     `json:"minNumber"`
	MaxNumber     int     `json:"maxNumber"`
	PoolSize      int     `json:"poolSize"`
	MaxAttempts   int     `json:"maxAttempts"`
	Constrained   bool    `json:"constrained"`
}

// toConfigJSON 把生效配置转成对外结构。
func toConfigJSON(cfg config.Config) configJSON {
	return configJSON{
		DrawCount:     cfg.DrawCount,
		SpecialNumber: cfg.SpecialNumber,
		SpecialProb:   cfg.SpecialProb,
		MinNumber:     cfg.MinNumber,
		MaxNumber:     cfg.MaxNumber,
		PoolSize:      cfg.PoolSize(),
		MaxAttempts:   cfg.MaxAttempts,
		Constrained:   cfg.Constrained,
	}
}

// scenarioJSON 描述一个玩法模式。
type scenarioJSON struct {
	Name        string  `json:"name"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	SpecialProb float64 `json:"specialProb"`
	Constrained bool    `json:"constrained"`
}

// scenarioInfoJSON 把模式的元数据转成对外结构。
func scenarioInfoJSON(m scenario.Info) scenarioJSON {
	return scenarioJSON{
		Name:        string(m.Name),
		Title:       m.Title,
		Description: m.Description,
		SpecialProb: m.Prob,
		Constrained: m.Constrained,
	}
}

// weightJSON 是单个号码的单次命中权重。
type weightJSON struct {
	Number int     `json:"number"`
	Weight float64 `json:"weight"`
	IsSpec bool    `json:"isSpecial"`
}

// countJSON 是某个非特殊号码在一轮中的票数。
type countJSON struct {
	Number int `json:"number"`
	Count  int `json:"count"`
}

// drawJSON 是一轮抽奖结果。
type drawJSON struct {
	Items        []int        `json:"items"`
	Stats        engine.Stats `json:"stats"`
	Attempts     int          `json:"attempts"`
	Fallback     bool         `json:"fallback"`
	SpecialCount int          `json:"specialCount"`
	OtherCounts  []countJSON  `json:"otherCounts"`
}

// summaryJSON 是批量抽奖的汇总。
type summaryJSON struct {
	Rounds         int     `json:"rounds"`
	AverageAttempt float64 `json:"averageAttempt"`
	FallbackRounds int     `json:"fallbackRounds"`
}

// drawResponse 是 /api/draw 的响应。
//
// 单轮请求时 Items/Stats/Attempts 是那一轮的结果，Draws 为 nil；
// 批量请求时这三个字段是最后一轮的结果，完整列表在 Draws 里，汇总在 Summary 里。
type drawResponse struct {
	ID        string       `json:"id"`
	Timestamp int64        `json:"timestamp"`
	ElapsedMS float64      `json:"elapsedMs"`
	Scenario  scenarioJSON `json:"scenario"`
	Config    configJSON   `json:"config"`
	Items     []int        `json:"items"`
	Stats     engine.Stats `json:"stats"`
	Attempts  int          `json:"attempts"`
	Fallback  bool         `json:"fallback"`
	Summary   *summaryJSON `json:"summary,omitempty"`
	Draws     []drawJSON   `json:"draws,omitempty"`
}

// metaResponse 是 /api/meta 的响应，供前端渲染配置面板与自检。
type metaResponse struct {
	Config         configJSON     `json:"config"`
	Modes          []scenarioJSON `json:"modes"`
	Weights        []weightJSON   `json:"weights"`
	AcceptProb     float64        `json:"acceptProb"`
	ExpectedRounds float64        `json:"expectedRounds"`
	Feasible       bool           `json:"feasible"`
	Uptime         int64          `json:"uptimeSeconds"`
	ServerTime     int64          `json:"serverTime"`
}

// simDistJSON 是模拟结果中单个号码的分布数据。
type simDistJSON struct {
	Number    int     `json:"number"`
	Count     int     `json:"count"`
	Share     float64 `json:"share"`
	IsSpecial bool    `json:"isSpecial"`
}

// simulateResponse 是 /api/simulate 的响应：实测分布 vs 理论值。
type simulateResponse struct {
	Rounds             int           `json:"rounds"`
	DrawCount          int           `json:"drawCount"`
	Config             configJSON    `json:"config"`
	AcceptProbTheory   float64       `json:"acceptProbTheory"`
	ExpectedRounds     float64       `json:"expectedRounds"`
	AverageAttempt     float64       `json:"averageAttempt"`
	SpecialTopRate     float64       `json:"specialTopRate"`
	SpecialSeenRate    float64       `json:"specialSeenRate"`
	SpecialCountSpread map[int]int   `json:"specialCountSpread"`
	Distribution       []simDistJSON `json:"distribution"`
	ElapsedMS          float64       `json:"elapsedMs"`
}
