package service

import (
	"os"
	"testing"
	"time"
)

func TestPricingCatalogDownloadedFixture(t *testing.T) {
	path := os.Getenv("AIOFCAGE_PRICING_FIXTURE")
	if path == "" {
		t.Skip("设置 AIOFCAGE_PRICING_FIXTURE 解析下载的公开目录")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := parsePricingCatalog(data, time.Now())
	if err != nil || len(items) < 100 {
		t.Fatalf("公开目录解析失败: %d %v", len(items), err)
	}
	t.Logf("成功解析 %d 项有效文本模型价格", len(items))
}

func TestPricingCatalogRequiresKnownPricesAndKeepsCurrencies(t *testing.T) {
	data := []byte(`{"sample":{"name":"样例供应商","models":{
"priced":{"cost":{"input":2,"output":8,"cache_read":0.2},"modalities":{"output":["text"]}},
"free":{"cost":{"input":0,"output":0}},
"missing":{"cost":{"input":1}},
"image":{"cost":{"input":2,"output":8},"modalities":{"output":["image"]}},
"bad":{"cost":{"input":-1,"output":8}}
}}}`)
	items, err := parsePricingCatalog(data, time.Unix(10, 0))
	if err != nil || len(items) != 2 {
		t.Fatalf("%+v %v", items, err)
	}
	for _, item := range items {
		if item.Pricing.Currency != "USD" || item.Pricing.Source == "" || item.Pricing.AsOf == "" {
			t.Fatal("缺少价格单位或来源")
		}
		if item.Model == "priced" && (item.Pricing.CacheRead != 0.2 || item.Pricing.CacheWrite != 2) {
			t.Fatal("缺失缓存价不能按免费计算")
		}
	}
}
