// Command draw 在命令行里跑抽奖，不需要启动 HTTP 服务。
//
// 适用场景：本地验证参数、批量压测、把抽奖接到别的脚本里（输出 JSON）。
//
// 用法：
//
//	go run ./cmd/draw                    # 抽一轮
//	go run ./cmd/draw -n 5               # 连抽 5 轮
//	go run ./cmd/draw -json              # 以 JSON 输出，方便管道处理
//	go run ./cmd/draw -bench             # 压测：跑 20000 轮并打印统计
//	go run ./cmd/draw -simulate 100000   # 蒙特卡洛自检
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/engine"
	"github.com/RazorEdge-wyh/lucky-draw/internal/randsource"
)

func main() {
	var (
		rounds     = flag.Int("n", 1, "抽几轮")
		asJSON     = flag.Bool("json", false, "以 JSON 输出")
		doBench    = flag.Bool("bench", false, "压测模式：跑 20000 轮并输出统计")
		simulate   = flag.Int("simulate", 0, "蒙特卡洛自检轮数（0 表示关闭）")
		showConfig = flag.Bool("config", false, "打印生效配置与权重后退出")
	)
	flag.Parse()

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "配置非法: %v\n", err)
		os.Exit(1)
	}

	// 命令行工具复用环境变量配置，端口在这里没有意义，予以忽略。
	eng, err := engine.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "构造引擎失败: %v\n", err)
		os.Exit(1)
	}

	if *showConfig {
		printConfig(cfg, eng)
		return
	}

	switch {
	case *doBench:
		runBench(eng, 20000)
	case *simulate > 0:
		runSimulate(eng, *simulate)
	default:
		runDraws(eng, *rounds, *asJSON)
	}
}

func printConfig(cfg config.Config, eng *engine.Engine) {
	fmt.Printf("配置：一轮 %d 次，号码池 %d..%d，特殊号码 %d，权重 %.4f，约束 %v\n",
		cfg.DrawCount, cfg.MinNumber, cfg.MaxNumber, cfg.SpecialNumber,
		cfg.SpecialProb, cfg.Constrained)
	for i, n := range numberRange(cfg) {
		mark := " "
		if n == cfg.SpecialNumber {
			mark = "*"
		}
		fmt.Printf("  %s 号码 %d：单次权重 %.4f\n", mark, n, eng.Weights()[i])
	}
	p := eng.AcceptProb()
	fmt.Printf("单轮自然命中率 %.6f → 期望重抽 %.2f 轮；参数可行：%v\n",
		p, 1/p, eng.Feasible())
}

func runDraws(eng *engine.Engine, rounds int, asJSON bool) {
	type round struct {
		Items    []int        `json:"items"`
		Stats    engine.Stats `json:"stats"`
		Attempts int          `json:"attempts"`
		Fallback bool         `json:"fallback"`
	}

	results := make([]round, 0, rounds)
	start := time.Now()
	for i := 0; i < rounds; i++ {
		res, err := eng.DrawChecked()
		if err != nil {
			fmt.Fprintf(os.Stderr, "抽奖失败: %v\n", err)
			os.Exit(1)
		}
		results = append(results, round{
			Items:    res.Items,
			Stats:    eng.Stats(res.Items),
			Attempts: res.Attempts,
			Fallback: res.Fallback,
		})
	}
	elapsed := time.Since(start)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"rounds":  results,
			"id":      randsource.NewID(),
			"elapsed": elapsed.Milliseconds(),
		})
		return
	}

	for i, r := range results {
		fmt.Printf("第 %d 轮  %v   （内部重抽 %d 轮）\n", i+1, r.Items, r.Attempts)
		if r.Fallback {
			fmt.Printf("  警告：该轮由兜底构造产生，说明参数过紧\n")
		}
	}
	if rounds > 1 {
		fmt.Printf("共 %d 轮，耗时 %s，单轮平均 %s\n",
			rounds, elapsed, elapsed/time.Duration(rounds))
	}
}

func runBench(eng *engine.Engine, rounds int) {
	start := time.Now()
	attemptSum := 0
	fallbacks := 0
	for i := 0; i < rounds; i++ {
		res, err := eng.DrawChecked()
		if err != nil {
			fmt.Fprintf(os.Stderr, "抽奖失败: %v\n", err)
			os.Exit(1)
		}
		attemptSum += res.Attempts
		if res.Fallback {
			fallbacks++
		}
	}
	elapsed := time.Since(start)

	fmt.Printf("压测 %d 轮完成\n", rounds)
	fmt.Printf("  总耗时      %s\n", elapsed)
	fmt.Printf("  单轮耗时    %s\n", elapsed/time.Duration(rounds))
	fmt.Printf("  吞吐        %.0f 轮/秒\n", float64(rounds)/elapsed.Seconds())
	fmt.Printf("  平均重抽    %.3f 轮（解析期望 %.3f 轮）\n",
		float64(attemptSum)/float64(rounds), 1/eng.AcceptProb())
	fmt.Printf("  兜底触发    %d 轮\n", fallbacks)
}

func runSimulate(eng *engine.Engine, rounds int) {
	cfg := eng.Config()
	dist := make(map[int]int, cfg.PoolSize())
	topRate := 0
	attemptSum := 0

	start := time.Now()
	for i := 0; i < rounds; i++ {
		res, err := eng.DrawChecked()
		if err != nil {
			fmt.Fprintf(os.Stderr, "抽奖失败: %v\n", err)
			os.Exit(1)
		}
		attemptSum += res.Attempts
		st := eng.Stats(res.Items)
		for _, n := range res.Items {
			dist[n]++
		}
		if st.TopNumber == cfg.SpecialNumber {
			topRate++
		}
	}
	elapsed := time.Since(start)

	totalDraws := float64(rounds * cfg.DrawCount)
	fmt.Printf("蒙特卡洛自检：%d 轮 × %d 次 = %.0f 次抽取，耗时 %s\n",
		rounds, cfg.DrawCount, totalDraws, elapsed)
	fmt.Printf("  特殊号码成为唯一最高票的比例  %.4f\n", float64(topRate)/float64(rounds))
	fmt.Printf("  平均内部重抽轮数              %.3f（解析期望 %.3f）\n",
		float64(attemptSum)/float64(rounds), 1/eng.AcceptProb())
	fmt.Println("  各号码实际出现占比：")
	for _, n := range numberRange(cfg) {
		mark := " "
		if n == cfg.SpecialNumber {
			mark = "*"
		}
		fmt.Printf("    %s 号码 %d：%.5f\n", mark, n, float64(dist[n])/totalDraws)
	}
}

func numberRange(cfg config.Config) []int {
	out := make([]int, 0, cfg.PoolSize())
	for n := cfg.MinNumber; n <= cfg.MaxNumber; n++ {
		out = append(out, n)
	}
	return out
}
