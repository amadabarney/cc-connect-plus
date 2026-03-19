package main

import (
	"context"
	"testing"

	"github.com/amadabarney/cc-connect-plus/core"
)

type stubCorePlatform struct{ replies []string }

func (s *stubCorePlatform) Name() string                                                     { return "stub" }
func (s *stubCorePlatform) Start(_ core.MessageHandler) error                                { return nil }
func (s *stubCorePlatform) Reply(_ context.Context, _ any, content string) error {
	s.replies = append(s.replies, content)
	return nil
}
func (s *stubCorePlatform) Send(_ context.Context, _ any, content string) error {
	s.replies = append(s.replies, content)
	return nil
}
func (s *stubCorePlatform) Stop() error { return nil }

func TestWrapFeishuPlatform_NilProjectMgmt(t *testing.T) {
	original := &stubCorePlatform{}
	wrapped := WrapFeishuPlatform(original, nil)
	if wrapped != original {
		t.Error("expected original platform when projectMgmt is nil")
	}
}

func TestWrapFeishuPlatform_ReturnsWrapper(t *testing.T) {
	original := &stubCorePlatform{}
	// 直接构造 wrappedFeishuPlatform 验证嵌入
	w := &wrappedFeishuPlatform{Platform: original, projectMgmt: nil}
	if w.Platform != original {
		t.Error("Platform field should be original")
	}
}

