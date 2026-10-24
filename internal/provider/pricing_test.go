package provider

import "testing"

func TestPricingSeparatesCachedInput(t *testing.T) {
	p := Pricing{Currency: "CNY", Input: 2, Output: 8, CacheRead: 0.2, CacheWrite: 2.5}
	cost, ok := p.Cost(1000000, 100000, 200000, 100000)
	// 700000*2 + 100000*8 + 200000*.2 + 100000*2.5，除以百万。
	if !ok || cost != 2.49 {
		t.Fatalf("%f %v", cost, ok)
	}
	if _, ok := p.Cost(10, 0, 11, 0); ok {
		t.Fatal("重复缓存计价")
	}
	p.Currency = "JPY"
	if p.Validate() == nil {
		t.Fatal("无效币种")
	}
}
