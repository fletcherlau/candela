package config

import "testing"

// FEISHU_PUSH_CHAT_ID 支持逗号分隔多个会话：多群推送。
func TestLoadParsesCommaSeparatedChatIDs(t *testing.T) {
	t.Setenv("FEISHU_PUSH_CHAT_ID", "oc_aaa, oc_bbb,,")
	c := Load()
	want := []string{"oc_aaa", "oc_bbb"}
	if len(c.FeishuPushChatIDs) != len(want) {
		t.Fatalf("FeishuPushChatIDs = %v, want %v", c.FeishuPushChatIDs, want)
	}
	for i, id := range want {
		if c.FeishuPushChatIDs[i] != id {
			t.Fatalf("FeishuPushChatIDs[%d] = %q, want %q", i, c.FeishuPushChatIDs[i], id)
		}
	}
}

// 单个 chat_id（无逗号）保持向后兼容。
func TestLoadSingleChatIDBackwardCompatible(t *testing.T) {
	t.Setenv("FEISHU_PUSH_CHAT_ID", "oc_aaa")
	c := Load()
	if len(c.FeishuPushChatIDs) != 1 || c.FeishuPushChatIDs[0] != "oc_aaa" {
		t.Fatalf("FeishuPushChatIDs = %v, want [oc_aaa]", c.FeishuPushChatIDs)
	}
}
