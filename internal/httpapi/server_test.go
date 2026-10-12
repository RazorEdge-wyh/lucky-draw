package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
)

// newTestServer 返回一个用默认配置装配好的处理器。
func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("测试配置不合法: %v", err)
	}
	return New(cfg).Handler()
}

// do 发起一次请求并返回状态码与响应体。
func do(t *testing.T, h http.Handler, method, target string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	return rec.Code, body
}

// TestHealthz 存活探针必须稳定返回 200 ok。
func TestHealthz(t *testing.T) {
	h := newTestServer(t)
	code, body := do(t, h, http.MethodGet, "/healthz")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	if string(body) != "ok" {
		t.Errorf("响应体应为 ok，实际 %q", body)
	}
}

// TestIndexServesConsole 首页必须返回内置控制台 HTML。
func TestIndexServesConsole(t *testing.T) {
	h := newTestServer(t)
	code, body := do(t, h, http.MethodGet, "/")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	html := string(body)
	for _, want := range []string{"<!DOCTYPE html>", "/app.js", "幸运抽奖"} {
		if !strings.Contains(html, want) {
			t.Errorf("首页应包含 %q", want)
		}
	}
}

// TestAppJSServed 前端脚本必须带正确的 MIME 并且非空。
func TestAppJSServed(t *testing.T) {
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type 应为 javascript，实际 %q", ct)
	}
	if rec.Body.Len() < 1000 {
		t.Errorf("app.js 内容过短，可能没嵌入成功: %d 字节", rec.Body.Len())
	}
}

// TestUnknownPathIs404 未注册路径必须 404，不能误返回首页。
func TestUnknownPathIs404(t *testing.T) {
	h := newTestServer(t)
	if code, _ := do(t, h, http.MethodGet, "/nope"); code != http.StatusNotFound {
		t.Errorf("未知路径应 404，实际 %d", code)
	}
	// 路径穿越尝试也必须被挡住。
	if code, _ := do(t, h, http.MethodGet, "/web/../go.mod"); code == http.StatusOK {
		t.Error("路径穿越不应返回 200")
	}
}

// TestDrawReturnsValidPayload 抽奖接口的结构与不变量。
func TestDrawReturnsValidPayload(t *testing.T) {
	h := newTestServer(t)

	for i := 0; i < 30; i++ {
		code, body := do(t, h, http.MethodGet, "/api/draw")
		if code != http.StatusOK {
			t.Fatalf("状态码应为 200，实际 %d，响应 %s", code, body)
		}

		var resp drawResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}

		if len(resp.ID) != 32 {
			t.Errorf("ID 应为 32 位十六进制，实际 %q", resp.ID)
		}
		if len(resp.Items) != 9 {
			t.Fatalf("items 长度应为 9，实际 %d", len(resp.Items))
		}
		if resp.Stats.SpecialCount == 0 || resp.Stats.TopNumber != 4 {
			t.Fatalf("特殊号码未被保证为唯一最高票: items=%v stats=%+v", resp.Items, resp.Stats)
		}
		if resp.Config.SpecialNumber != 4 || resp.Config.MaxNumber != 8 {
			t.Errorf("响应中的配置不正确: %+v", resp.Config)
		}
		if resp.ElapsedMS < 0 {
			t.Errorf("耗时不应为负: %v", resp.ElapsedMS)
		}
		if resp.Draws != nil {
			t.Error("单轮请求不应返回 draws 数组")
		}
		// 号码必须落在号码池内。
		for _, n := range resp.Items {
			if n < 1 || n > 8 {
				t.Fatalf("号码越界: %d", n)
			}
		}
	}
}

// TestDrawBatch 批量抽奖返回 10 轮与汇总信息。
func TestDrawBatch(t *testing.T) {
	h := newTestServer(t)
	code, body := do(t, h, http.MethodGet, "/api/draw?count=10")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d，响应 %s", code, body)
	}

	var resp drawResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if len(resp.Draws) != 10 {
		t.Fatalf("应返回 10 轮结果，实际 %d", len(resp.Draws))
	}
	if resp.Summary == nil || resp.Summary.Rounds != 10 {
		t.Fatalf("缺少批量汇总: %+v", resp.Summary)
	}
	if resp.Summary.AverageAttempt < 1 {
		t.Errorf("平均重抽轮数应 >= 1，实际 %v", resp.Summary.AverageAttempt)
	}
	for i, d := range resp.Draws {
		if len(d.Items) != 9 {
			t.Errorf("第 %d 轮长度错误: %d", i, len(d.Items))
		}
		if d.SpecialCount == 0 {
			t.Errorf("第 %d 轮特殊号码未出现: %v", i, d.Items)
		}
	}
}

// TestDrawParameterValidation 非法参数必须返回 400 并说明原因，而不是 500。
func TestDrawParameterValidation(t *testing.T) {
	h := newTestServer(t)
	cases := []struct {
		name   string
		target string
		want   string
	}{
		{"轮数超上限", "/api/draw?count=9999", "count"},
		{"轮数为零", "/api/draw?count=0", "count"},
		{"负轮数", "/api/draw?count=-5", "count"},
		{"轮内次数超上限", "/api/draw?drawCount=100000", "drawCount"},
		{"概率越界", "/api/draw?specialProb=2", "specialProb"},
		{"概率非数字", "/api/draw?specialProb=abc", "specialProb"},
		{"未知模式", "/api/draw?scenario=nope", "nope"},
		{"号码池上界过小", "/api/draw?maxNumber=1", "maxNumber"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := do(t, h, http.MethodGet, tc.target)
			if code != http.StatusBadRequest {
				t.Fatalf("状态码应为 400，实际 %d，响应 %s", code, body)
			}
			var e errorResponse
			if err := json.Unmarshal(body, &e); err != nil {
				t.Fatalf("错误响应不是合法 JSON: %v", err)
			}
			if e.Status != http.StatusBadRequest {
				t.Errorf("错误体里的 status 应为 400，实际 %d", e.Status)
			}
			if !strings.Contains(e.Error, tc.want) {
				t.Errorf("错误信息应包含 %q，实际 %q", tc.want, e.Error)
			}
		})
	}
}

// TestDrawRejectsBadMethod 不支持的 HTTP 方法必须 405。
func TestDrawRejectsBadMethod(t *testing.T) {
	h := newTestServer(t)
	if code, _ := do(t, h, http.MethodDelete, "/api/draw"); code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE 应返回 405，实际 %d", code)
	}
	// POST 是被允许的。
	if code, _ := do(t, h, http.MethodPost, "/api/draw"); code != http.StatusOK {
		t.Errorf("POST 应返回 200，实际 %d", code)
	}
}

// TestMetaExposesWeightsAndModes 元信息接口要能支撑前端首屏渲染。
func TestMetaExposesWeightsAndModes(t *testing.T) {
	h := newTestServer(t)
	code, body := do(t, h, http.MethodGet, "/api/meta")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}

	var meta metaResponse
	if err := json.Unmarshal(body, &meta); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if len(meta.Weights) != 8 {
		t.Fatalf("权重表长度应为 8，实际 %d", len(meta.Weights))
	}
	sum := 0.0
	specialWeight := -1.0
	for _, w := range meta.Weights {
		sum += w.Weight
		if w.IsSpec {
			specialWeight = w.Weight
		}
	}
	if diff := sum - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("权重之和应为 1，实际 %.12f", sum)
	}
	if specialWeight != 0.15 {
		t.Errorf("特殊号码权重应为 0.15，实际 %v", specialWeight)
	}
	if len(meta.Modes) != 3 {
		t.Errorf("应暴露 3 个模式，实际 %d", len(meta.Modes))
	}
	if meta.AcceptProb <= 0 || meta.AcceptProb >= 1 {
		t.Errorf("单轮命中率应在 (0,1) 内，实际 %v", meta.AcceptProb)
	}
	// 默认参数下单轮命中率约 0.111，期望重抽约 9 轮。
	if meta.AcceptProb < 0.08 || meta.AcceptProb > 0.15 {
		t.Errorf("默认参数单轮命中率应约 0.11，实际 %v", meta.AcceptProb)
	}
	if !meta.Feasible {
		t.Error("默认参数应当可行")
	}
	if meta.ExpectedRounds <= 1 {
		t.Errorf("期望重抽轮数应大于 1，实际 %v", meta.ExpectedRounds)
	}
}

// TestSimulateMatchesTheory 模拟接口的实测分布要贴近理论权重。
func TestSimulateMatchesTheory(t *testing.T) {
	h := newTestServer(t)
	code, body := do(t, h, http.MethodGet, "/api/simulate?rounds=3000")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d，响应 %s", code, body)
	}

	var sim simulateResponse
	if err := json.Unmarshal(body, &sim); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if sim.Rounds != 3000 {
		t.Errorf("模拟轮数应为 3000，实际 %d", sim.Rounds)
	}
	if len(sim.Distribution) != 8 {
		t.Fatalf("分布条目应为 8，实际 %d", len(sim.Distribution))
	}

	// 约束模式下特殊号码 100% 是唯一最高票。
	if sim.SpecialTopRate != 1 {
		t.Errorf("特殊号码最高票率应为 1，实际 %v", sim.SpecialTopRate)
	}
	// 平均重抽轮数必须与解析期望一致（默认参数约 9 轮）。
	if diff := sim.AverageAttempt - sim.ExpectedRounds; diff > 0.5 || diff < -0.5 {
		t.Errorf("平均重抽 %.2f 与期望 %.2f 偏差过大", sim.AverageAttempt, sim.ExpectedRounds)
	}
	if sim.ExpectedRounds < 3 || sim.ExpectedRounds > 20 {
		t.Errorf("默认参数的期望重抽轮数应落在 3..20，实际 %.2f", sim.ExpectedRounds)
	}
	// 特殊号码占比应高于任何一个其他号码（因为它必然至少 2 票）。
	var specialShare, maxOtherShare float64
	for _, d := range sim.Distribution {
		if d.IsSpecial {
			specialShare = d.Share
		} else if d.Share > maxOtherShare {
			maxOtherShare = d.Share
		}
	}
	if specialShare <= maxOtherShare {
		t.Errorf("特殊号码占比 %.4f 应高于其他号码最大值 %.4f", specialShare, maxOtherShare)
	}
	// 模拟不应污染线上统计口径之外的任何东西；这里只确认没有兜底告警路径。
	if sim.SpecialSeenRate != 1 {
		t.Errorf("约束模式下特殊号码出现率应为 1，实际 %v", sim.SpecialSeenRate)
	}
}

// TestSimulateRoundsValidation 模拟轮数超限必须 400。
func TestSimulateRoundsValidation(t *testing.T) {
	h := newTestServer(t)
	for _, target := range []string{"/api/simulate?rounds=0", "/api/simulate?rounds=999999"} {
		if code, body := do(t, h, http.MethodGet, target); code != http.StatusBadRequest {
			t.Errorf("%s 应返回 400，实际 %d（%s）", target, code, body)
		}
	}
}

// TestOutOfRangeMaxNumberRejected 号码池上界太小（池里凑不出可用的其他号码）时，
// 必须在参数校验阶段就以 400 拒绝，而不是等到引擎里报 500。
func TestOutOfRangeMaxNumberRejected(t *testing.T) {
	h := newTestServer(t)
	for _, target := range []string{"/api/draw?maxNumber=1", "/api/draw?maxNumber=0"} {
		code, body := do(t, h, http.MethodGet, target)
		if code != http.StatusBadRequest {
			t.Fatalf("%s 应返回 400，实际 %d，响应 %s", target, code, body)
		}
		if !strings.Contains(string(body), "maxNumber") {
			t.Errorf("错误信息应提到 maxNumber，实际 %s", body)
		}
	}
}

// TestSmallPoolStillWorks 号码池缩到 2 个号码仍然必须能正常抽奖：
// 可行性的下限是「池里至少 2 个号码」，不是「其他号码足够多」。
func TestSmallPoolStillWorks(t *testing.T) {
	h := newTestServer(t)
	for i := 0; i < 20; i++ {
		code, body := do(t, h, http.MethodGet, "/api/draw?maxNumber=2")
		if code != http.StatusOK {
			t.Fatalf("号码池 1..2 应当可抽，实际 %d：%s", code, body)
		}
		var resp drawResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if len(resp.Items) != 9 {
			t.Fatalf("items 长度应为 9，实际 %d", len(resp.Items))
		}
		// 号码池只有 1、2，特殊号码被夹到 2，且它必须是唯一最高票。
		for _, n := range resp.Items {
			if n < 1 || n > 2 {
				t.Fatalf("号码越界（池为 1..2）: %d", n)
			}
		}
		if resp.Stats.TopNumber != 2 || resp.Stats.SpecialCount == 0 {
			t.Fatalf("特殊号码（被夹到 2）应为唯一最高票: items=%v stats=%+v", resp.Items, resp.Stats)
		}
	}
}

// TestFairScenarioIsUnconstrained 等概率模式必须跳过整轮约束。
func TestFairScenarioIsUnconstrained(t *testing.T) {
	h := newTestServer(t)
	code, body := do(t, h, http.MethodGet, "/api/draw?scenario=fair")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d，响应 %s", code, body)
	}

	var resp drawResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp.Config.Constrained {
		t.Error("等概率模式不应启用约束")
	}
	if resp.Attempts != 1 {
		t.Errorf("等概率模式不应重抽，实际 %d 轮", resp.Attempts)
	}
	// 权重应均等：8 个号码 → 每个 0.125。
	if diff := resp.Config.SpecialProb - 0.125; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("等概率模式权重应为 0.125，实际 %v", resp.Config.SpecialProb)
	}
}

// TestSpecialProbOverride 单次请求覆盖权重必须真正生效。
func TestSpecialProbOverride(t *testing.T) {
	h := newTestServer(t)
	code, body := do(t, h, http.MethodGet, "/api/draw?specialProb=0.5")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d，响应 %s", code, body)
	}
	var resp drawResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp.Config.SpecialProb != 0.5 {
		t.Errorf("权重覆盖未生效，实际 %v", resp.Config.SpecialProb)
	}
}

// TestSecurityHeaders 基础安全响应头必须存在。
func TestSecurityHeaders(t *testing.T) {
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/draw", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	for k, want := range map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("响应头 %s 应为 %q，实际 %q", k, want, got)
		}
	}
}

// TestEngineCacheReuse 相同参数的重复请求应复用同一个引擎实例。
func TestEngineCacheReuse(t *testing.T) {
	cfg := config.Default()
	srv := New(cfg)

	req := request{Scenario: "challenging", Count: 1}
	e1, _, _, err := srv.engineFor(req)
	if err != nil {
		t.Fatalf("第一次构造失败: %v", err)
	}
	e2, _, _, err := srv.engineFor(req)
	if err != nil {
		t.Fatalf("第二次构造失败: %v", err)
	}
	if e1 != e2 {
		t.Error("相同参数应命中缓存并复用同一引擎")
	}

	// 不同参数必须产生不同引擎。
	e3, _, _, err := srv.engineFor(request{Scenario: "hot", Count: 1})
	if err != nil {
		t.Fatalf("加权模式构造失败: %v", err)
	}
	if e3 == e1 {
		t.Error("不同参数不应复用同一引擎")
	}
}

// TestClientIP 客户端地址解析要优先信任反代写入的原地址。
func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:12345"
	if got := clientIP(req); got != "10.0.0.9" {
		t.Errorf("应解析出 10.0.0.9，实际 %q", got)
	}

	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.7" {
		t.Errorf("应取 XFF 中第一个地址，实际 %q", got)
	}
}
