package pack

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

// TestOfficialKeysWired 发版信任链路门禁：官方公钥必须已内置且命中受信名单；
// 非名单公钥必须仍被拒（门禁不因发版而放宽）。重复装配幂等（同进程多实例安全）。
func TestOfficialKeysWired(t *testing.T) {
	if len(officialPubkeys) == 0 {
		t.Fatal("官方发布者名单为空——远程安装信任链路未配置，发版不完整")
	}
	if err := RegisterOfficialKeys(); err != nil {
		t.Fatalf("官方名单装配失败: %v", err)
	}
	if err := RegisterOfficialKeys(); err != nil {
		t.Fatalf("重复装配必须幂等: %v", err)
	}
	for id, pub := range officialPubkeys {
		got, err := TrustPubkey(pub)
		if err != nil || got != id {
			t.Errorf("官方公钥 %s 应命中受信名单: %v %q", id, err, got)
		}
		k, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := TrustPubkey(hex.EncodeToString(k)); !errors.Is(err, ErrUntrustedKey) {
			t.Errorf("名单外公钥必须拒收: %v", err)
		}
	}
}
