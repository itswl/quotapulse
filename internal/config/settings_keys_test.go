package config

import "testing"

func TestAPIKeysSplitsAndTrims(t *testing.T) {
	s := &Settings{WebAPIKey: " primary , secondary ,,"}
	keys := s.APIKeys()
	if len(keys) != 2 || keys[0] != "primary" || keys[1] != "secondary" {
		t.Fatalf("多 key 解析不对: %#v", keys)
	}
	if len((&Settings{}).APIKeys()) != 0 {
		t.Fatal("空 WEB_API_KEY 应解析为空列表")
	}
}
