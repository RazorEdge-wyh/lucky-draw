// Package httpapi 提供极简的抽奖接口：一次请求返回一个号码。
//
//	GET  /api/draw   抽下一个号码（首次请求自动开始新一轮）
//	GET  /api/reset  放弃当前进度，重新开始一轮
//	GET  /healthz    存活探针
//	GET  /           内置页面
//
// 「一轮九次、特殊号码必定是唯一最高票」这个约束由 engine 保证：
// 每条结果序列都是一次性生成好的，接口只是把它按顺序一个一个交付出去。
// 因此不存在「抽到第八次发现凑不齐、只能重来」的情况 —— 整轮早在第一次
// 请求时就已经确定并满足约束了。
package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/engine"
	"github.com/RazorEdge-wyh/lucky-draw/internal/randsource"
)

const (
	// sessionCookie 保存浏览器会话标识，用于记住「本轮抽到第几个了」。
	sessionCookie = "ld_sid"

	// sessionMax 限制同时存在的会话数，防止无人清理的旧会话把内存吃满。
	sessionMax = 10000

	// sessionTTL 是会话空闲多久后可以被回收。
	// 一轮只有九次，正常用户几秒就抽完；两小时纯粹是留给「过一会儿再来抽」的场景。
	sessionTTL = 2 * time.Hour

	// cleanupEvery 是后台清理的间隔。
	cleanupEvery = 10 * time.Minute
)

// session 是某个浏览器当前那一轮的进度。
type session struct {
	// seq 是本轮已经生成好的完整号码序列，长度等于 DrawCount。
	seq []int
	// pos 是下一个要交付的下标。
	pos int
	// rounds 是该会话累计完成的轮数。
	rounds int
	// lastSeen 用于空闲回收。
	lastSeen time.Time
}

// store 是内存会话表。单实例部署足够；多副本时各副本各管各的会话。
type store struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func newStore() *store {
	return &store{sessions: make(map[string]*session)}
}

// get 取出会话并刷新其活跃时间。
func (s *store) get(id string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	sess.lastSeen = time.Now()
	return sess, true
}

// put 写入（或覆盖）一个会话。
func (s *store) put(id string, sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.lastSeen = time.Now()
	s.sessions[id] = sess
}

// drop 删除一个会话。
func (s *store) drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// gc 回收空闲过久的会话，返回回收数量。
func (s *store) gc(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := 0
	for id, sess := range s.sessions {
		if now.Sub(sess.lastSeen) > sessionTTL {
			delete(s.sessions, id)
			dropped++
		}
	}
	return dropped
}

func (s *store) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// evictIfNeeded 在会话数超限时先清过期项；若仍然超限就整表清空。
// 宁可让个别用户重新开始一轮，也不能让内存无限增长。
func (s *store) evictIfNeeded() {
	if s.size() <= sessionMax {
		return
	}
	s.gc(time.Now())
	if s.size() > sessionMax {
		s.mu.Lock()
		n := len(s.sessions)
		s.sessions = make(map[string]*session)
		s.mu.Unlock()
		log.Printf("warn: 会话数超过 %d，已清空全部会话（清理了 %d 条）", sessionMax, n)
	}
}

// Server 组装 HTTP 处理器。
type Server struct {
	engine *engine.Engine
	cfg    config.Config
	store  *store
}

// New 构造 HTTP 服务。base 来自环境变量。
func New(base config.Config) (*Server, error) {
	eng, err := engine.New(base)
	if err != nil {
		return nil, err
	}
	s := &Server{engine: eng, cfg: base, store: newStore()}
	go s.cleanupLoop()
	return s, nil
}

// cleanupLoop 定期回收空闲会话。
func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for range ticker.C {
		if n := s.store.gc(time.Now()); n > 0 {
			log.Printf("cleanup: 回收了 %d 个空闲会话，剩余 %d 个", n, s.store.size())
		}
	}
}

// Handler 返回带中间件的最终处理器。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/draw", s.handleDraw)
	mux.HandleFunc("/api/reset", s.handleReset)
	mux.HandleFunc("/app.js", s.handleAsset)
	mux.HandleFunc("/", s.handleIndex)

	return s.recoverer(s.logger(s.securityHeaders(mux)))
}

// ---------------------------------------------------------------------------
// 响应结构
// ---------------------------------------------------------------------------

// drawResponse 是一次抽取的响应。
type drawResponse struct {
	// Number 是本次抽出的号码。
	Number int `json:"number"`
	// Index 是本次是这一轮的第几次，取值 1..Total。
	Index int `json:"index"`
	// Total 是一轮的次数。
	Total int `json:"total"`
	// RoundDone 表示这一轮是否已经抽满。
	RoundDone bool `json:"roundDone"`
	// Round 是本会话当前的轮数（从 1 开始）。
	Round int `json:"round"`
}

type errorResponse struct {
	Error  string `json:"error"`
	Status int    `json:"status"`
}

// ---------------------------------------------------------------------------
// 处理器
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleDraw 交付本轮的下一个号码。
func (s *Server) handleDraw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET / POST")
		return
	}

	id, sess := s.sessionFor(r)
	// 两种情况需要现开一轮：
	//   - sess == nil：新访客，或旧会话已过期；
	//   - pos >= len(seq)：上一轮已经抽满九次，这一次点击属于下一轮。
	//
	// 第二种情况要是漏了，就会在轮次边界上越界 panic —— 这是真实踩过的坑。
	if sess == nil {
		id = randsource.NewID()
		sess = &session{}
		s.setCookie(w, id)
	}
	if sess.pos >= len(sess.seq) {
		sess.seq = s.engine.DrawSequence()
		sess.pos = 0
	}

	n := sess.seq[sess.pos]
	sess.pos++
	roundDone := sess.pos >= len(sess.seq)

	// 轮数：整轮结束后才 +1，因此进行中的这一轮就是 rounds+1。
	roundNumber := sess.rounds + 1
	if roundDone {
		sess.rounds++
	}

	s.store.put(id, sess)
	s.store.evictIfNeeded()

	log.Printf("draw sid=%s number=%d index=%d/%d round=%d done=%t",
		shortID(id), n, sess.pos, len(sess.seq), roundNumber, roundDone)

	writeJSON(w, http.StatusOK, drawResponse{
		Number:    n,
		Index:     sess.pos,
		Total:     len(sess.seq),
		RoundDone: roundDone,
		Round:     roundNumber,
	})
}

// handleReset 放弃当前进度，下一次抽取会开新一轮。
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET / POST")
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		s.store.drop(c.Value)
	}
	s.setCookie(w, randsource.NewID())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// sessionFor 读取当前会话；没有或已过期时返回 nil。
func (s *Server) sessionFor(r *http.Request) (string, *session) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return "", nil
	}
	sess, ok := s.store.get(c.Value)
	if !ok {
		// 会话已过期：清掉这个 Cookie，让调用方重新开始。
		return "", nil
	}
	return c.Value, sess
}

func (s *Server) setCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ---------------------------------------------------------------------------
// 静态资源
// ---------------------------------------------------------------------------

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

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.js" {
		http.NotFound(w, r)
		return
	}
	data, err := asset("web/app.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// 中间件与工具
// ---------------------------------------------------------------------------

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

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// 健康检查与静态资源太频繁，不记日志以免淹没有效信息。
		switch r.URL.Path {
		case "/healthz", "/app.js":
			return
		}
		log.Printf("http %s %s cost=%s", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("error: 响应编码失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg, Status: status})
}

// shortID 只取会话 ID 的前 8 位打日志，避免日志里出现完整标识。
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
