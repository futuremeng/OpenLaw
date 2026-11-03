// official.go 官方发布者密钥名单：发版时内置（AGENTS.md 信任与签名纪律 16.2）。
// 空名单 = 如实现状：官方远程通道尚未发布，远程目录与包一律按「来源不可信」拒绝——
// 门禁不因未发布而放宽。轮换用新 key ID；吊销删除旧 ID。
package pack

// officialPubkeys key ID → ed25519 公钥 hex（发版流水线填入）。
// ol-2026a：v0.1.0 首发密钥（2026-11-02 生成；私钥存仓库与数据目录之外，
// 只经路径引用——AGENTS.md 16.2）。轮换用新 key ID；吊销删除旧 ID。
var officialPubkeys = map[string]string{
	"ol-2026a": "9a9a4cc8b4ce003a3c7382eab4557a668c6813c3e5bf556efc7ad6d45931ca01",
}

// RegisterOfficialKeys 把官方密钥登记进受信名单（桌面启动与 CLI 入口调用；
// 重复登记同 ID 会报错——同进程只调一次）。
func RegisterOfficialKeys() error {
	for id, pub := range officialPubkeys {
		if err := RegisterTrustedPubkey(id, pub); err != nil {
			return err
		}
	}
	return nil
}
