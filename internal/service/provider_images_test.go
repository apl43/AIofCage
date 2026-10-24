package service

import (
	"testing"

	"github.com/shaoxier/AIofCage/internal/provider"
	"github.com/shaoxier/AIofCage/internal/session"
)

func TestDSHImageCapabilitySurvivesEditingAndCopy(t *testing.T) {
	app, _, st := newApp(t)
	request := SaveProviderRequest{Name: "视觉模型", Agent: session.DSH, Protocol: provider.Anthropic,
		BaseURL: "https://example.com/anthropic", Model: "vision-model", Auth: provider.AuthXAPIKey,
		APIKey: "test-only-key", ImageInput: true}
	saved, err := app.SaveProvider(request)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := st.Provider(saved.ID)
	if !saved.ImageInput || !persisted.ImageInput {
		t.Fatal("图片能力没有保存")
	}
	copy, err := app.DuplicateProvider(saved.ID)
	if err != nil || !copy.ImageInput {
		t.Fatalf("复制供应商丢失图片能力：%v", err)
	}
	request.ID, request.APIKey, request.ImageInput = saved.ID, "", false
	updated, err := app.SaveProvider(request)
	if err != nil || updated.ImageInput {
		t.Fatalf("不能关闭图片能力：%v", err)
	}
}
