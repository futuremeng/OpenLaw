// Package validate 校验层（开发文档 3.5、6、9.2、13.4、16.2）：
// 断链检测、manifest 生成与签名、引用校验。断链不抛异常，统一走结构化结果。
package validate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/futuremeng/OpenLaw/hashutil"
)

// ToolVersion 打包/签名工具版本，写入 manifest（16.2）。
const ToolVersion = "openlaw-pack/0.1.0"

// 签名校验相关哨兵错误
var (
	ErrManifestMissing  = errors.New("manifest 缺失")
	ErrFileMismatch     = errors.New("文件与 manifest 不一致（被替换或篡改）")
	ErrSignatureInvalid = errors.New("manifest 签名验证失败")
	ErrKeyMissing       = errors.New("签名私钥缺失")
	ErrPubKeyMissing    = errors.New("验签公钥缺失")
)

// FileEntry manifest 中的单个文件条目。
type FileEntry struct {
	Path   string `json:"path"` // 相对数据目录，如 pdf/xxx.pdf
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest 每日汇总：所有 PDF 路径与 SHA-256（4.3 防篡改机制）。
type Manifest struct {
	GeneratedAt string      `json:"generated_at"`
	ToolVersion string      `json:"tool_version"`
	Files       []FileEntry `json:"files"`
}

// ManifestPath 返回数据目录下 manifest 相关路径。
func manifestPaths(dataDir string) (jsonPath, sigPath, pubPath string) {
	dir := filepath.Join(dataDir, "manifest")
	return filepath.Join(dir, "manifest.json"), filepath.Join(dir, "manifest.sig"), filepath.Join(dir, "pubkey.hex")
}

// GenerateKey 生成 Ed25519 私钥并保存到 path（hex 编码 seed）。
// 私钥不进入代码仓库、不进入数据目录（16.2）。
func GenerateKey(path string) error {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(seed)), 0o600)
}

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrKeyMissing
	}
	seed, err := hex.DecodeString(string(b))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("私钥文件格式错误: %s", path)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// BuildManifest 扫描 dataDir/pdf 下全部文件，重算 SHA-256 生成 manifest.json，
// 使用私钥签名写入 manifest.sig，公钥写入 manifest/pubkey.hex。
func BuildManifest(dataDir, keyPath string) error {
	pdfDir := filepath.Join(dataDir, "pdf")
	entries := []FileEntry{}
	err := filepath.Walk(pdfDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		sha, herr := hashutil.SHA256File(p)
		if herr != nil {
			return herr
		}
		rel, rerr := filepath.Rel(dataDir, p)
		if rerr != nil {
			return rerr
		}
		entries = append(entries, FileEntry{Path: filepath.ToSlash(rel), Size: info.Size(), SHA256: sha})
		return nil
	})
	if err != nil {
		return fmt.Errorf("扫描 pdf 目录失败: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	m := Manifest{GeneratedAt: time.Now().UTC().Format(time.RFC3339), ToolVersion: ToolVersion, Files: entries}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	priv, err := loadPrivateKey(keyPath)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(priv, body)

	jsonPath, sigPath, pubPath := manifestPaths(dataDir)
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(sigPath, []byte(hex.EncodeToString(sig)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(pubPath, []byte(hex.EncodeToString(priv.Public().(ed25519.PublicKey))), 0o644)
}

// LoadManifest 读取 manifest.json 与条目索引。
func LoadManifest(dataDir string) (*Manifest, map[string]FileEntry, error) {
	jsonPath, _, _ := manifestPaths(dataDir)
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, nil, ErrManifestMissing
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, nil, fmt.Errorf("manifest 解析失败: %w", err)
	}
	index := make(map[string]FileEntry, len(m.Files))
	for _, e := range m.Files {
		index[e.Path] = e
	}
	return &m, index, nil
}

// VerifyManifest 校验 manifest 签名并逐条重算文件哈希（9.2 每日校验）。
// 返回哨兵错误：ErrManifestMissing / ErrSignatureInvalid / ErrFileMismatch。
func VerifyManifest(dataDir string) error {
	jsonPath, sigPath, pubPath := manifestPaths(dataDir)
	body, err := os.ReadFile(jsonPath)
	if err != nil {
		return ErrManifestMissing
	}
	pubHex, err := os.ReadFile(pubPath)
	if err != nil {
		return ErrPubKeyMissing
	}
	pub, err := hex.DecodeString(string(pubHex))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ErrPubKeyMissing
	}
	sigHex, err := os.ReadFile(sigPath)
	if err != nil {
		return ErrSignatureInvalid
	}
	sig, err := hex.DecodeString(string(sigHex))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), body, sig) {
		return ErrSignatureInvalid
	}

	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return ErrSignatureInvalid
	}
	for _, e := range m.Files {
		p := filepath.Join(dataDir, filepath.FromSlash(e.Path))
		sha, err := hashutil.SHA256File(p)
		if err != nil || sha != e.SHA256 {
			return fmt.Errorf("%w: %s", ErrFileMismatch, e.Path)
		}
	}
	return nil
}

// ManifestEntryFor 查询 manifest 中某相对路径的条目。
func ManifestEntryFor(dataDir, relPath string) (*FileEntry, error) {
	_, index, err := LoadManifest(dataDir)
	if err != nil {
		return nil, err
	}
	e, ok := index[relPath]
	if !ok {
		return nil, fmt.Errorf("%w: manifest 无条目 %s", ErrFileMismatch, relPath)
	}
	return &e, nil
}
