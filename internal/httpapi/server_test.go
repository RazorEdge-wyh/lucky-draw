package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
)

// newTestHandler 返回一个用默认配置装配好的处理器。
func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("测试配置不合法: %v", err)
	}
	api, err := New(cfg)
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}
	return api.Handler()
}

// client 模拟一个浏览器：持有会话 Cookie，并串联后续请求。
type client struct {
	h      http.Handler
	cookie string
}

func newClient(h http.Handler) *client { return &client{h: h} }

// get 发起一次请求，自动带上已有的会话 Cookie，并记录响应下发的新 Cookie。
func (c *client) get(t *testing.T, target string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookie {
			c.cookie = sessionCookie + "=" + ck.Value
		}
	}
	return rec.Code, body
}

// post 发起一次 POST 请求，同样维护 Cookie。
func (c *client) post(t *testing.T, target string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, nil)
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookie {
			c.cookie = sessionCookie + "=" + ck.Value
		}
	}
	return rec.Code, body
}

// drawOnce 抽一次并解析响应。
func (c *client) drawOnce(t *testing.T) drawResponse {
	t.Helper()
	code, body := c.get(t, "/api/draw")
	if code != http.StatusOK {
		t.Fatalf("抽取失败: HTTP %d（%s）", code, body)
	}
	var resp drawResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（原文 %s）", err, body)
	}
	return resp
}

// TestHealthz 存活探针。
func TestHealthz(t *testing.T) {
	h := newTestHandler(t)
	code, body := newClient(h).get(t, "/healthz")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	if string(body) != "ok" {
		t.Errorf("响应体应为 ok，实际 %q", body)
	}
}

// TestIndexIsMinimal 首页只应保留标题、号码、按钮和本轮结果，
// 用户明确要求删掉的功能不能再出现在页面上。
func TestIndexIsMinimal(t *testing.T) {
	h := newTestHandler(t)
	code, body := newClient(h).get(t, "/")
	if code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", code)
	}
	html := string(body)

	for _, want := range []string{"<!DOCTYPE html>", "随机数生成器", `id="draw"`, `id="number"`, `id="result"`, "/app.js"} {
		if !strings.Contains(html, want) {
			t.Errorf("首页应包含 %q", want)
		}
	}

	// 结果区的号码必须统一样式：不能再为特殊号码单独加高亮。
	if strings.Contains(html, "i.special") {
		t.Error("结果区不应再为特殊号码单独设置高亮样式（i.special）")
	}
	if !strings.Contains(html, "i.placeholder") {
		t.Error("结果区应保留未抽取位置的虚线占位样式")
	}

	// 只在「用户可见的文本」里检查已删功能。不能直接扫原始源码：
	// 注释和脚本里必然会提到这些词（例如「本轮结果」的实现说明），
	// 那样会把正常代码误判成残留功能。
	visible := stripScriptsAndComments(html)
	for _, unwanted := range []string{"连抽", "十连", "概率自检", "票数统计", "请求日志", "玩法", "特殊号码"} {
		if strings.Contains(visible, unwanted) {
			t.Errorf("页面可见文本里不应再包含已删除的功能：%q", unwanted)
		}
	}
}

// stripScriptsAndComments 去掉 <script>、<style> 与 HTML 注释，
// 只留下用户真正能看到的文本，供上面的检查使用。
//
// 注意必须连 <style> 一起去掉：CSS 注释也是「代码」而不是可见文本，
// 漏掉它会把注释里的词误判成残留功能（这个坑真实踩过）。
func stripScriptsAndComments(html string) string {
	s := html
	for _, tag := range []string{"script", "style"} {
		open := "<" + tag
		closeTag := "</" + tag + ">"
		for {
			start := strings.Index(s, open)
			if start < 0 {
				break
			}
			rel := strings.Index(s[start:], closeTag)
			if rel < 0 {
				s = s[:start]
				break
			}
			s = s[:start] + s[start+rel+len(closeTag):]
		}
	}
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			break
		}
		rel := strings.Index(s[start:], "-->")
		if rel < 0 {
			break
		}
		s = s[:start] + s[start+rel+len("-->"):]
	}
	return s
}

// TestAppJSServed 前端脚本必须带正确 MIME 且非空。
func TestAppJSServed(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type 应为 javascript，实际 %q", ct)
	}
	if rec.Body.Len() < 500 {
		t.Errorf("app.js 内容过短，可能没嵌入成功: %d 字节", rec.Body.Len())
	}
}

// TestRemovedEndpointsAreGone 已删除的接口与不存在的路径都必须 404。
func TestRemovedEndpointsAreGone(t *testing.T) {
	h := newTestHandler(t)
	for _, p := range []string{"/nope", "/api/meta", "/api/simulate", "/web/../go.mod"} {
		if code, _ := newClient(h).get(t, p); code == http.StatusOK {
			t.Errorf("%s 不应返回 200", p)
		}
	}
}

// TestFirstDrawStartsNewRound 第一次抽取应当开始新一轮并下发会话 Cookie。
func TestFirstDrawStartsNewRound(t *testing.T) {
	h := newTestHandler(t)
	c := newClient(h)
	resp := c.drawOnce(t)

	if resp.Index != 1 {
		t.Errorf("新一轮第一次抽取的 index 应为 1，实际 %d", resp.Index)
	}
	if resp.Total != 9 {
		t.Errorf("一轮应为 9 次，实际 total=%d", resp.Total)
	}
	if resp.Round != 1 {
		t.Errorf("轮数应从 1 开始，实际 %d", resp.Round)
	}
	if resp.RoundDone {
		t.Error("第一次抽取不应标记本轮结束")
	}
	if resp.Number < 1 || resp.Number > 8 {
		t.Errorf("号码应落在 1..8，实际 %d", resp.Number)
	}
	if c.cookie == "" {
		t.Fatal("响应应当下发会话 Cookie")
	}
}

// TestUniformRoundIsFairAndComplete 默认模式是**均匀随机**。
//
// 一轮九次、每次只返回一个号码，号码在 1..8 之间均匀分布：
//   - 没有任何号码享有「保证出现」的特权（4 也可能整轮不出现，这是正常的）；
//   - 每个号码的总体占比应当接近 1/8。
//
// 这个测试就是「真随机」的守护者：如果有人重新引入偏向，它会失败。
func TestUniformRoundIsFairAndComplete(t *testing.T) {
	h := newTestHandler(t)

	const rounds = 400
	dist := map[int]int{}
	totalDraws := 0

	for roundNo := 1; roundNo <= rounds; roundNo++ {
		c := newClient(h) // 每轮换一个全新"浏览器"，确保是独立的一轮
		items := make([]int, 0, 9)

		for i := 1; i <= 9; i++ {
			resp := c.drawOnce(t)

			if resp.Index != i {
				t.Fatalf("第 %d 轮第 %d 次的 index 应为 %d，实际 %d", roundNo, i, i, resp.Index)
			}
			if resp.RoundDone != (i == 9) {
				t.Fatalf("第 %d 轮第 %d 次 roundDone 应为 %t，实际 %t",
					roundNo, i, i == 9, resp.RoundDone)
			}
			if resp.Number < 1 || resp.Number > 8 {
				t.Fatalf("号码越界: %d", resp.Number)
			}
			items = append(items, resp.Number)
			dist[resp.Number]++
			totalDraws++
		}

		if len(items) != 9 {
			t.Fatalf("第 %d 轮应有 9 个号码，实际 %d", roundNo, len(items))
		}
	}

	// 每个号码都应出现过，且占比接近 1/8。
	// 400 轮 × 9 次 = 3600 次抽取，单个号码占比的标准差约 0.0055，
	// 因此 1/8 ± 0.04 是很宽松的区间，不会误报，但足以抓出明显偏向。
	want := 1.0 / 8.0
	for n := 1; n <= 8; n++ {
		if dist[n] == 0 {
			t.Errorf("号码 %d 从未出现（均匀随机下不该发生）", n)
		}
		share := float64(dist[n]) / float64(totalDraws)
		if diff := share - want; diff > 0.04 || diff < -0.04 {
			t.Errorf("号码 %d 占比 %.4f，偏离均匀值 %.4f 过多（可能存在偏向）", n, share, want)
		}
	}

	// 关键回归断言：4 的占比必须**接近 1/8，而不是有偏模式下的约 37%**。
	share4 := float64(dist[4]) / float64(totalDraws)
	if share4 > 0.2 {
		t.Errorf("号码 4 占比 %.4f 明显偏高，像是又启用了有偏模式（应为 %.4f）", share4, want)
	}
}

// TestBiasedModeStillWorksWhenEnabled 备份下来的有偏算法必须仍然可用：
// 显式设置 SPECIAL_NUMBER 后，接口应当恢复「每轮必中该号码」的行为。
func TestBiasedModeStillWorksWhenEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.SpecialNumber = 4 // 显式启用有偏模式
	if !cfg.Weighted() {
		t.Fatal("测试配置应当处于有偏模式")
	}

	api, err := New(cfg)
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}
	h := api.Handler()

	for roundNo := 1; roundNo <= 40; roundNo++ {
		c := newClient(h)
		items := make([]int, 0, 9)
		for i := 1; i <= 9; i++ {
			items = append(items, c.drawOnce(t).Number)
		}

		counts := map[int]int{}
		for _, n := range items {
			counts[n]++
		}
		if counts[4] == 0 {
			t.Fatalf("有偏模式下第 %d 轮没有命中 4：%v", roundNo, items)
		}
		for n, cnt := range counts {
			if n != 4 && cnt >= counts[4] {
				t.Fatalf("有偏模式下第 %d 轮 4 不是唯一最高票：%v", roundNo, items)
			}
		}
	}
}

// TestRoundIsNineDrawsAndFourAlwaysWinsLegacy 保留一条针对有偏模式的最严格断言，
// 直接驱动引擎层，确保被备份的算法在 80 轮里都不出错。
func TestRoundIsNineDrawsAndFourAlwaysWinsLegacy(t *testing.T) {
	cfg := config.Default()
	cfg.SpecialNumber = 4
	api, err := New(cfg)
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}

	for roundNo := 1; roundNo <= 80; roundNo++ {
		seq := api.engine.DrawSequence()
		if len(seq) != 9 {
			t.Fatalf("第 %d 轮长度应为 9，实际 %d", roundNo, len(seq))
		}
		st := api.engine.Stats(seq)
		if st.SpecialCount == 0 || st.TopNumber != 4 {
			t.Fatalf("第 %d 轮 4 未成为唯一最高票：%v", roundNo, seq)
		}
	}
}

// TestConsecutiveRoundsInOneSession 同一个会话连续抽 18 次应当跨越两轮，
// 第二轮的轮数递增，每轮都是完整的 9 次。
//
// 注意这里刻意**不**断言 4 一定出现：默认是均匀随机，4 整轮不出现是正常的。
func TestConsecutiveRoundsInOneSession(t *testing.T) {
	h := newTestHandler(t)
	c := newClient(h)

	for roundNo := 1; roundNo <= 2; roundNo++ {
		items := make([]int, 0, 9)
		for i := 1; i <= 9; i++ {
			resp := c.drawOnce(t)
			if resp.Round != roundNo {
				t.Fatalf("第 %d 轮第 %d 次返回的轮数应为 %d，实际 %d", roundNo, i, roundNo, resp.Round)
			}
			if resp.Index != i {
				t.Fatalf("第 %d 轮第 %d 次的 index 应为 %d，实际 %d", roundNo, i, i, resp.Index)
			}
			if resp.Number < 1 || resp.Number > 8 {
				t.Fatalf("号码越界: %d", resp.Number)
			}
			items = append(items, resp.Number)
		}
		if len(items) != 9 {
			t.Fatalf("第 %d 轮应有 9 个号码，实际 %d", roundNo, len(items))
		}
	}
}

// TestResetRestartsRound 重置后应当从第 1 次重新开始。
func TestResetRestartsRound(t *testing.T) {
	h := newTestHandler(t)
	c := newClient(h)

	// 先抽三次
	for i := 0; i < 3; i++ {
		c.drawOnce(t)
	}

	if code, body := c.post(t, "/api/reset"); code != http.StatusOK {
		t.Fatalf("重置应返回 200，实际 %d（%s）", code, body)
	}

	resp := c.drawOnce(t)
	if resp.Index != 1 {
		t.Errorf("重置后第一次抽取的 index 应为 1，实际 %d", resp.Index)
	}
}

// TestDrawRejectsBadMethod 不支持的 HTTP 方法必须 405。
func TestDrawRejectsBadMethod(t *testing.T) {
	h := newTestHandler(t)
	c := newClient(h)
	req := httptest.NewRequest(http.MethodDelete, "/api/draw", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE 应返回 405，实际 %d", rec.Code)
	}
	if code, _ := c.post(t, "/api/draw"); code != http.StatusOK {
		t.Errorf("POST 应返回 200，实际 %d", code)
	}
}

// TestSecurityHeaders 基础安全响应头必须存在。
func TestSecurityHeaders(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/draw", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("响应头 %s 应为 %q，实际 %q", k, want, got)
		}
	}
}

// TestSessionStoreGC 过期会话必须被回收，避免内存无限增长。
func TestSessionStoreGC(t *testing.T) {
	s := newStore()
	old := &session{seq: []int{1}}
	old.lastSeen = time.Now().Add(-3 * sessionTTL)
	s.sessions["old"] = old
	s.put("fresh", &session{seq: []int{1}})

	if n := s.gc(time.Now()); n != 1 {
		t.Errorf("应回收 1 个过期会话，实际 %d", n)
	}
	if s.size() != 1 {
		t.Errorf("回收后应剩 1 个会话，实际 %d", s.size())
	}
	if _, ok := s.get("old"); ok {
		t.Error("过期会话不应还能取到")
	}
	if _, ok := s.get("fresh"); !ok {
		t.Error("未过期会话应当保留")
	}
}

// TestSessionStoreEviction 会话数超限时必须能自愈。
func TestSessionStoreEviction(t *testing.T) {
	s := newStore()
	for i := 0; i < sessionMax+10; i++ {
		s.put(randomKey(i), &session{seq: []int{1}})
	}
	s.evictIfNeeded()
	if s.size() > sessionMax {
		t.Errorf("清理后会话数应不超过 %d，实际 %d", sessionMax, s.size())
	}
}

// randomKey 生成互不相同的测试键。
func randomKey(i int) string {
	const digits = "0123456789abcdef"
	return string([]byte{
		digits[(i>>12)&0xf], digits[(i>>8)&0xf], digits[(i>>4)&0xf], digits[i&0xf],
	})
}
