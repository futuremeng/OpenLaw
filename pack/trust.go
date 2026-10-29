// trust.go 发布者信任层（借鉴 edgeever windows-update-trust 的内置公钥模型）。
// 背景：Verify 用包内 pubkey.hex 验签，属自签自验——只能证明包内容完整一致，
// 不能证明是谁签的；任何人可用自己的密钥对产出"通过校验"的包。
// 信任层规定：导入默认要求包公钥命中内置受信名单（来源可信），
// 未命中一律拒绝（完整性通过≠来源可信）。私钥始终存于仓库外（16.2）。
// 轮换：新增 key ID；吊销：删除旧 key ID。名单在发版时维护，
// 下游自部署若要信任自签包，在自己的装配脚本中调用 RegisterTrustedPubkey，
// 或使用 ImportAllowUntrusted 显式本地自验（不推荐用于分发）。
package pack

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrUntrustedKey 包签名有效但公钥不在受信名单。
var ErrUntrustedKey = errors.New("更新包签名公钥不在受信发布者名单（完整性通过≠来源可信）")

// trustedPubkeys key ID → ed25519 公钥 hex。官方发布密钥随版本发布内置。
var trustedPubkeys = map[string]string{}

// RegisterTrustedPubkey 注册受信发布者公钥（发版内置、下游装配或测试使用）。
func RegisterTrustedPubkey(keyID, pubHex string) error {
	pub, err := hex.DecodeString(strings.TrimSpace(pubHex))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("公钥 hex 非法: %s", keyID)
	}
	if _, dup := trustedPubkeys[keyID]; dup {
		return fmt.Errorf("key ID 已存在: %s", keyID)
	}
	trustedPubkeys[keyID] = strings.ToLower(strings.TrimSpace(pubHex))
	return nil
}

// TrustOf 读取包内 pubkey.hex，返回命中的受信 key ID；未命中返回 ErrUntrustedKey。
func TrustOf(packDir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(packDir, "pubkey.hex"))
	if err != nil {
		return "", ErrSignatureFail
	}
	h := strings.ToLower(strings.TrimSpace(string(b)))
	if _, err := hex.DecodeString(h); err != nil {
		return "", ErrSignatureFail
	}
	for id, pub := range trustedPubkeys {
		if pub == h {
			return id, nil
		}
	}
	return "", ErrUntrustedKey
}

// VerifyTrusted 完整性校验 + 来源信任校验，两者都通过才返回 nil。
func VerifyTrusted(packDir string) error {
	if err := Verify(packDir); err != nil {
		return err
	}
	_, err := TrustOf(packDir)
	return err
}
