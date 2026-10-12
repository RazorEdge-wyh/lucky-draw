// Package config 集中管理服务配置。
//
// 所有参数都可以通过环境变量覆盖，方便在容器 / CI / 云平台上调参而不改代码。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config 是一次抽奖服务的完整配置。
type Config struct {
	// DrawCount 一轮抽多少次（默认 9）。
	DrawCount int
	// SpecialNumber 特殊号码，即奖品对应的那个号码（默认 4）。
	SpecialNumber int
	// SpecialProb 特殊号码单次命中的概率权重（默认 0.15）。
	SpecialProb float64
	// MinNumber 号码池下界（默认 1）。
	MinNumber int
	// MaxNumber 号码池上界（默认 8）。
	MaxNumber int
	// MaxAttempts 内部整轮重抽的上限（默认 5000）。
	MaxAttempts int
	// Constrained 为 true 时启用整轮约束：特殊号码必须是本轮唯一最多票。
	// 关闭后引擎退化为纯加权抽样，不做任何整轮筛选。
	Constrained bool
	// Port HTTP 监听端口（默认 8080）。
	Port string
}

// Default 返回不带任何环境变量时的默认配置。
func Default() Config {
	return Config{
		DrawCount:     9,
		SpecialNumber: 4,
		SpecialProb:   0.15,
		MinNumber:     1,
		MaxNumber:     8,
		MaxAttempts:   5000,
		Constrained:   true,
		Port:          "8080",
	}
}

// Load 从环境变量读取配置，未设置的项使用默认值。
func Load() Config {
	c := Default()
	c.DrawCount = EnvInt("DRAW_COUNT", c.DrawCount)
	c.SpecialNumber = EnvInt("SPECIAL_NUMBER", c.SpecialNumber)
	c.SpecialProb = EnvFloat("SPECIAL_PROB", c.SpecialProb)
	c.MinNumber = EnvInt("MIN_NUMBER", c.MinNumber)
	c.MaxNumber = EnvInt("MAX_NUMBER", c.MaxNumber)
	c.MaxAttempts = EnvInt("MAX_ATTEMPTS", c.MaxAttempts)
	c.Constrained = EnvBool("CONSTRAINED", c.Constrained)
	c.Port = EnvString("PORT", c.Port)
	return c
}

// Validate 检查配置是否自洽。返回的错误可以直接展示给运维人员。
func (c Config) Validate() error {
	if c.MinNumber > c.MaxNumber {
		return fmt.Errorf("MIN_NUMBER(%d) 不能大于 MAX_NUMBER(%d)", c.MinNumber, c.MaxNumber)
	}
	// 先查号码池容量：池子太小的话，后面「特殊号码越界」之类的报错会掩盖真正原因。
	if c.MaxNumber-c.MinNumber < 1 {
		return fmt.Errorf("号码池至少要有 2 个号码，当前 [%d, %d] 只有 %d 个",
			c.MinNumber, c.MaxNumber, c.MaxNumber-c.MinNumber+1)
	}
	if c.SpecialNumber < c.MinNumber || c.SpecialNumber > c.MaxNumber {
		return fmt.Errorf("SPECIAL_NUMBER(%d) 必须落在 [MIN_NUMBER, MAX_NUMBER] = [%d, %d] 区间内",
			c.SpecialNumber, c.MinNumber, c.MaxNumber)
	}
	if c.SpecialProb < 0 || c.SpecialProb > 1 {
		return fmt.Errorf("SPECIAL_PROB(%v) 必须落在 [0, 1] 区间内", c.SpecialProb)
	}
	if c.DrawCount < 1 {
		return fmt.Errorf("DRAW_COUNT(%d) 必须大于 0", c.DrawCount)
	}
	if c.MaxAttempts < 1 {
		return fmt.Errorf("MAX_ATTEMPTS(%d) 必须大于 0", c.MaxAttempts)
	}
	if c.Port == "" {
		return fmt.Errorf("PORT 不能为空")
	}
	return nil
}

// Others 返回号码池中除特殊号码以外的全部号码，升序。
func (c Config) Others() []int {
	others := make([]int, 0, c.MaxNumber-c.MinNumber)
	for n := c.MinNumber; n <= c.MaxNumber; n++ {
		if n != c.SpecialNumber {
			others = append(others, n)
		}
	}
	return others
}

// PoolSize 返回号码池大小。
func (c Config) PoolSize() int { return c.MaxNumber - c.MinNumber + 1 }

// EnvString 读取字符串环境变量，为空时返回默认值。
func EnvString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// EnvInt 读取整型环境变量，解析失败时返回默认值。
func EnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// EnvFloat 读取浮点环境变量，解析失败时返回默认值。
func EnvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// EnvBool 读取布尔环境变量，接受 1/0、true/false、yes/no、on/off，解析失败时返回默认值。
func EnvBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "":
		return def
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}
