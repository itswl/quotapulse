package store

import (
	"math"
	"testing"
)

// limitOf 是所有历史查询 limit 的收口处：HTTP 与 MCP 在调用前各自夹过一次，这里保证
// 以后的调用方即使直接传大数，也不会溢出成一个负的 LIMIT。
func TestLimitOfClamps(t *testing.T) {
	cases := []struct {
		name     string
		limit    int
		fallback int
		want     int64
	}{
		{"零取默认值", 0, 50, 50},
		{"负数取默认值", -8, 50, 50},
		{"正常值原样保留", 200, 50, 200},
		{"超过上限被夹住", maxRowLimit + 1, 50, maxRowLimit},
		{"远超上限仍然被夹住", 1 << 40, 50, maxRowLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := limitOf(c.limit, c.fallback); got != c.want {
				t.Errorf("limitOf(%d, %d) = %d, 期望 %d", c.limit, c.fallback, got, c.want)
			}
		})
	}
}

// int32 转换必须夹住而不是环绕：环绕后传给驱动的是负 LIMIT，查询会直接失败。
func TestRowLimit32Clamps(t *testing.T) {
	cases := []struct {
		name  string
		limit int64
		want  int32
	}{
		{"零", 0, 0},
		{"正常值", 1000, 1000},
		{"int32 上界", math.MaxInt32, math.MaxInt32},
		{"超出 int32 上界", int64(math.MaxInt32) + 1, math.MaxInt32},
		{"负值归零", -5, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rowLimit32(c.limit); got != c.want {
				t.Errorf("rowLimit32(%d) = %d, 期望 %d", c.limit, got, c.want)
			}
		})
	}
}
