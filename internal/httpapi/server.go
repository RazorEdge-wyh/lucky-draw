// Package httpapi 提供抽奖服务的 HTTP 接口。
//
// 接口一览：
//
//	GET  /healthz       存活探针
//	GET  /api/meta      当前配置、模式列表与权重表（前端首屏自检用）
//	GET  /api/draw      抽奖（默认 1 轮，?count=N 可批量）
//	GET  /api/simulate  蒙特卡洛自检：跑 N 轮并返回实测分布
//	GET  /              内置网页控制台
package httpapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/engine"
	"github.com/RazorEdge-wyh/lucky-draw/internal/randsource"
	"github.com/RazorEdge-wyh/lucky-draw/internal/scenario"
)

// 单次请求允许的最大抽奖轮数，防止 ?count=1000000 之类的请求打满 CPU。
const maxBatchCount = 100

// maxSimulateRounds 是自检接口允许的最大模拟轮数。
const maxSimulateRounds = 20000

// maxCachedEngines 限制参数变体缓存条数，避免被构造出来的任意参数撑爆内存。
const maxCachedEngines = 64

// maxDrawCount 是一次请求内「一轮抽多少次」的上限。
const maxDrawCount = 1000

// maxNumberLimit 是号码池上界的上限，防止构造出极端巨大的累积概率表。
const maxNumberLimit = 1000

// Server 组装 HTTP 处理器。
type Server struct {
	base  config.Config
	stats *metrics

	mu      sync.Mutex
	engines map[string]*engine.Engine
	order   []string
}

// New 构造 HTTP 服务。base 来自环境变量，是所有模式的基础配置。
func New(base config.Config) *Server {
	return &Server{
		base:    base,
		stats:   &metrics{startedAt: time.Now()},
		engines: make(map[string]*engine.Engine),
	}
}

// Handler 返回带中间件的最终处理器。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/draw", s.handleDraw)
	mux.HandleFunc("/api/simulate", s.handleSimulate)
	mux.HandleFunc("/app.js", s.handleAsset)
	mux.HandleFunc("/", s.handleIndex)

	return s.recoverer(s.logger(s.securityHeaders(mux)))
}

// ---------------------------------------------------------------------------
// 参数解析
// ---------------------------------------------------------------------------

// request 是一次抽奖请求解析后的参数。
//
// DrawCount / MaxNumber 用指针而不是「0 表示未指定」：
// 0 本身对客户端来说是一个可能被传进来的（非法）取值，
// 如果拿 0 当哨兵，?maxNumber=0 就会被静默当成「用默认值」而返回 200。
type request struct {
	Scenario scenario.Name
	// DrawCount 覆盖一轮抽多少次；nil 表示使用服务端配置。
	DrawCount *int
	// MaxNumber 覆盖号码池上界；nil 表示使用服务端配置。
	MaxNumber *int
	// SpecialProb 覆盖特殊号码权重；nil 表示使用模式默认值。
	SpecialProb *float64
	// Count 一次请求抽几轮。
	Count int
}

// parseRequest 从查询串解析参数，并做范围校验。
//
// 设计要点：用指针区分「参数没传」（走服务端默认值）和「传了但非法」（直接 400）。
// 如果两者混为一谈，?count=0 会被静默当成默认值，调用方拿到 200 却不知道自己传错了。
func (s *Server) parseRequest(r *http.Request) (request, error) {
	q := r.URL.Query()

	sc, err := scenario.Lookup(q.Get("scenario"))
	if err != nil {
		return request{}, err
	}

	req := request{Scenario: sc.Name, Count: 1}

	count, err := intParamStrict(q, "count")
	if err != nil {
		return request{}, err
	}
	if count != nil {
		req.Count = *count
	}
	if req.DrawCount, err = intParamStrict(q, "drawCount"); err != nil {
		return request{}, err
	}
	if req.MaxNumber, err = intParamStrict(q, "maxNumber"); err != nil {
		return request{}, err
	}

	if req.Count < 1 || req.Count > maxBatchCount {
		return request{}, fmt.Errorf("count 必须在 1..%d 之间，收到 %d", maxBatchCount, req.Count)
	}
	if req.DrawCount != nil && (*req.DrawCount < 1 || *req.DrawCount > maxDrawCount) {
		return request{}, fmt.Errorf("drawCount 必须在 1..%d 之间，收到 %d", maxDrawCount, *req.DrawCount)
	}
	if req.MaxNumber != nil && (*req.MaxNumber < s.base.MinNumber+1 || *req.MaxNumber > maxNumberLimit) {
		return request{}, fmt.Errorf("maxNumber 必须在 %d..%d 之间，收到 %d",
			s.base.MinNumber+1, maxNumberLimit, *req.MaxNumber)
	}
	if v := q.Get("specialProb"); v != "" {
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return request{}, fmt.Errorf("specialProb 不是合法数字：%q", v)
		}
		if f < 0 || f > 1 {
			return request{}, fmt.Errorf("specialProb 必须在 0..1 之间，收到 %v", f)
		}
		req.SpecialProb = &f
	}

	return req, nil
}

// engineFor 返回（并缓存）某个参数组合对应的引擎。
//
// 之所以缓存：每次调用 engine.New 都要重建累积分布表，批量抽奖时重复构建纯属浪费。
// 缓存以「影响引擎行为的全部参数」为键，因此命中缓存等价于命中同一套概率。
func (s *Server) engineFor(req request) (*engine.Engine, config.Config, scenario.Info, error) {
	base := s.base

	sc, err := scenario.Lookup(string(req.Scenario))
	if err != nil {
		return nil, base, scenario.Info{}, err
	}

	cfg := sc.Apply(base)
	if req.DrawCount != nil {
		cfg.DrawCount = *req.DrawCount
	}
	if req.MaxNumber != nil {
		cfg.MaxNumber = *req.MaxNumber
		// 缩小号码池后，特殊号码可能落在池外，必须重新夹取。
		if cfg.SpecialNumber > cfg.MaxNumber {
			cfg.SpecialNumber = cfg.MaxNumber
		}
	}
	if req.SpecialProb != nil {
		cfg.SpecialProb = *req.SpecialProb
	}
	if err := cfg.Validate(); err != nil {
		return nil, cfg, sc, err
	}

	key := cacheKey(cfg)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.engines[key]; ok {
		return e, cfg, sc, nil
	}
	e, err := engine.New(cfg)
	if err != nil {
		return nil, cfg, sc, err
	}
	if len(s.order) >= maxCachedEngines {
		delete(s.engines, s.order[0])
		s.order = s.order[1:]
	}
	s.engines[key] = e
	s.order = append(s.order, key)
	return e, cfg, sc, nil
}

func cacheKey(cfg config.Config) string {
	return fmt.Sprintf("%d|%d|%d|%.10f|%.10f|%d|%t",
		cfg.DrawCount, cfg.MinNumber, cfg.MaxNumber, cfg.SpecialProb,
		float64(cfg.SpecialNumber), cfg.MaxAttempts, cfg.Constrained)
}

// ---------------------------------------------------------------------------
// 处理器
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleDraw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET / POST")
		return
	}

	req, err := s.parseRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, cfg, sc, err := s.engineFor(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	start := time.Now()
	results := make([]drawJSON, 0, req.Count)
	attemptTotal := 0
	fallbackCount := 0

	for i := 0; i < req.Count; i++ {
		res, err := e.DrawChecked()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		attemptTotal += res.Attempts
		if res.Fallback {
			fallbackCount++
			// 兜底意味着参数过紧，正常线上不该出现，必须留下告警日志。
			log.Printf("warn: 抽奖触发兜底构造 scenario=%s cfg=%s", req.Scenario, cacheKey(cfg))
		}
		results = append(results, s.toDrawJSON(e, res))
	}

	elapsed := time.Since(start)
	id := randsource.NewID()
	s.stats.record(req.Count, elapsed)

	// 服务端日志：只记录最终结果与耗时，方便排查线上问题。
	log.Printf("draw id=%s scenario=%s count=%d attempts=%d cost=%s",
		id, req.Scenario, req.Count, attemptTotal, elapsed)

	resp := drawResponse{
		ID:        id,
		Timestamp: time.Now().UnixMilli(),
		ElapsedMS: float64(elapsed.Microseconds()) / 1000.0,
		Scenario:  scenarioInfoJSON(sc),
		Config:    toConfigJSON(cfg),
		Items:     results[0].Items,
		Stats:     results[0].Stats,
		Attempts:  results[0].Attempts,
		Fallback:  results[0].Fallback,
		Draws:     results,
	}
	if req.Count == 1 {
		resp.Draws = nil // 单轮请求不重复返回整个数组
	} else {
		resp.Summary = &summaryJSON{
			Rounds:         req.Count,
			AverageAttempt: float64(attemptTotal) / float64(req.Count),
			FallbackRounds: fallbackCount,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	e, cfg, _, err := s.engineFor(request{Scenario: scenario.Default(), Count: 1})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	weights := make([]weightJSON, 0, len(e.Weights()))
	for i, n := range numberRange(cfg) {
		w := e.Weights()[i]
		weights = append(weights, weightJSON{
			Number: n,
			Weight: w,
			IsSpec: n == cfg.SpecialNumber,
		})
	}

	modes := make([]scenarioJSON, 0, len(scenario.All()))
	for _, m := range scenario.All() {
		modes = append(modes, scenarioInfoJSON(m))
	}

	writeJSON(w, http.StatusOK, metaResponse{
		Config:         toConfigJSON(cfg),
		Modes:          modes,
		Weights:        weights,
		AcceptProb:     e.AcceptProb(),
		ExpectedRounds: expectedRounds(e.AcceptProb()),
		Feasible:       e.Feasible(),
		Uptime:         int64(time.Since(s.stats.startedAt).Seconds()),
		ServerTime:     time.Now().UnixMilli(),
	})
}

func (s *Server) handleSimulate(w http.ResponseWriter, r *http.Request) {
	req, err := s.parseRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rounds := intParam(r.URL.Query(), "rounds", 2000)
	if rounds < 1 || rounds > maxSimulateRounds {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("rounds 必须在 1..%d 之间，收到 %d", maxSimulateRounds, rounds))
		return
	}
	e, cfg, _, err := s.engineFor(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	start := time.Now()
	dist := make(map[int]int, cfg.PoolSize())  // 号码 -> 出现总次数
	specialTop := 0                            // 特殊号码成为唯一最多的轮数
	specialSeen := 0                           // 特殊号码至少出现一次的轮数
	attemptSum := 0                            // 累计内部重抽轮数
	spread := make(map[int]int, cfg.DrawCount) // 特殊号码出现 k 次 -> 轮数

	for i := 0; i < rounds; i++ {
		res, err := e.DrawChecked()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		attemptSum += res.Attempts
		st := e.Stats(res.Items)
		for _, n := range res.Items {
			dist[n]++
		}
		spread[st.SpecialCount]++
		if st.SpecialCount > 0 {
			specialSeen++
		}
		if st.TopNumber == cfg.SpecialNumber {
			specialTop++
		}
	}

	distOut := make([]simDistJSON, 0, len(dist))
	for _, n := range numberRange(cfg) {
		c := dist[n]
		distOut = append(distOut, simDistJSON{
			Number:    n,
			Count:     c,
			Share:     float64(c) / float64(rounds*cfg.DrawCount),
			IsSpecial: n == cfg.SpecialNumber,
		})
	}

	writeJSON(w, http.StatusOK, simulateResponse{
		Rounds:             rounds,
		DrawCount:          cfg.DrawCount,
		Config:             toConfigJSON(cfg),
		AcceptProbTheory:   e.AcceptProb(),
		ExpectedRounds:     expectedRounds(e.AcceptProb()),
		AverageAttempt:     float64(attemptSum) / float64(rounds),
		SpecialTopRate:     float64(specialTop) / float64(rounds),
		SpecialSeenRate:    float64(specialSeen) / float64(rounds),
		SpecialCountSpread: spread,
		Distribution:       distOut,
		ElapsedMS:          float64(time.Since(start).Microseconds()) / 1000.0,
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := asset("web/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "内置页面缺失: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// 中间件
// ---------------------------------------------------------------------------

// recoverer 保证单个请求的 panic 不会拖垮整个进程。
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("error: 请求 panic path=%s err=%v\n%s", r.URL.Path, rec, debug.Stack())
				writeError(w, http.StatusInternalServerError, "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logger 记录访问日志，并附上真实客户端地址。
func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// 健康检查太频繁，不记日志以免淹没有效信息。
		if r.URL.Path != "/healthz" {
			log.Printf("http %s %s from=%s cost=%s", r.Method, r.URL.Path,
				clientIP(r), time.Since(start))
		}
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// 反代链路上第一个地址才是原始客户端。
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------------------------------------------------------------------------
// 统计与工具
// ---------------------------------------------------------------------------

type metrics struct {
	mu        sync.Mutex
	startedAt time.Time
	total     int64
	elapsedUS int64
	slowest   time.Duration
}

func (m *metrics) record(n int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.total += int64(n)
	m.elapsedUS += d.Microseconds()
	if d > m.slowest {
		m.slowest = d
	}
}

// expectedRounds 把「单轮命中率」换算成期望重抽轮数 1/p。
func expectedRounds(p float64) float64 {
	if p <= 0 {
		return 0
	}
	return 1 / p
}

// numberRange 返回号码池的完整升序序列。
func numberRange(cfg config.Config) []int {
	out := make([]int, 0, cfg.PoolSize())
	for n := cfg.MinNumber; n <= cfg.MaxNumber; n++ {
		out = append(out, n)
	}
	return out
}

// intParam 读取整型查询参数，缺失或非法时返回默认值。
// 仅用于服务端内部默认值处理（如 rounds），不用于需要报错的入参校验。
func intParam(q map[string][]string, key string, def int) int {
	v, err := intParamStrict(q, key)
	if err != nil || v == nil {
		return def
	}
	return *v
}

// intParamStrict 读取整型查询参数。
// 返回值语义：nil 表示参数未出现（或为空串）；非 nil 表示调用方确实传了值。
func intParamStrict(q map[string][]string, key string) (*int, error) {
	vs, ok := q[key]
	if !ok || len(vs) == 0 || strings.TrimSpace(vs[0]) == "" {
		return nil, nil
	}
	raw := strings.TrimSpace(vs[0])
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s 不是合法整数：%q", key, raw)
	}
	return &n, nil
}

func (s *Server) toDrawJSON(e *engine.Engine, res *engine.Result) drawJSON {
	st := e.Stats(res.Items)
	counts := make([]countJSON, 0, len(st.Counts))
	for _, n := range e.Config().Others() {
		counts = append(counts, countJSON{Number: n, Count: st.Counts[n]})
	}
	return drawJSON{
		Items:        res.Items,
		Stats:        st,
		Attempts:     res.Attempts,
		Fallback:     res.Fallback,
		SpecialCount: st.SpecialCount,
		OtherCounts:  counts,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("error: 响应编码失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg, Status: status})
}
