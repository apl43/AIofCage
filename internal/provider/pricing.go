package provider

import (
	"errors"
	"math"
)

// Pricing 的单价单位为每百万 token，版本在保存时生成，随请求快照保留。
type Pricing struct {
	Currency   string  `json:"currency"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Version    string  `json:"version"`
	Source     string  `json:"source,omitempty"`
	AsOf       string  `json:"as_of,omitempty"`
}

func (p Pricing) Validate() error {
	if len(p.Source) > 512 || len(p.AsOf) > 40 {
		return errors.New("价格来源或日期过长")
	}
	if p.Currency != "CNY" && p.Currency != "USD" {
		return errors.New("价格币种请选择 CNY 或 USD")
	}
	for _, v := range []float64{p.Input, p.Output, p.CacheRead, p.CacheWrite} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1e6 {
			return errors.New("单价必须在 0 到 1000000 之间")
		}
	}
	return nil
}
func (p Pricing) Cost(input, output, read, write int64) (float64, bool) {
	if p.Validate() != nil || min(input, output, read, write) < 0 || read+write > input {
		return 0, false
	}
	cost := (float64(input-read-write)*p.Input + float64(output)*p.Output + float64(read)*p.CacheRead + float64(write)*p.CacheWrite) / 1e6
	return math.Round(cost*1e8) / 1e8, true
}
