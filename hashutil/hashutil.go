// Package hashutil 哈希计算（开发文档 4.3）。
// 防篡改优先 SHA-256 / SHA-512 / 国密 SM3 并必须注明算法类型；
// MD5 仅用于判断下载损坏，不作为防篡改依据。
// 独立成包以避免 ingest ↔ validate 循环依赖。
package hashutil

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// HashFile 计算文件的 SHA-256（防篡改依据）与 MD5（仅判下载损坏），返回文件大小。
func HashFile(path string) (sha256Hex, md5Hex string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", 0, fmt.Errorf("读取文件失败: %w", err)
	}
	defer f.Close()

	sh, mh := sha256.New(), md5.New()
	w := io.MultiWriter(sh, mh)
	n, err := io.Copy(w, f)
	if err != nil {
		return "", "", 0, fmt.Errorf("计算哈希失败: %w", err)
	}
	return hex.EncodeToString(sh.Sum(nil)), hex.EncodeToString(mh.Sum(nil)), n, nil
}

// SHA256File 仅计算 SHA-256（校验场景复用）。
func SHA256File(path string) (string, error) {
	sha, _, _, err := HashFile(path)
	return sha, err
}
