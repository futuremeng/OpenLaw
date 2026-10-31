// catalog.go 更新包目录（index.json + index.sig）：远程分发（GitHub Releases 等）的
// 包清单。目录本身用与包相同的 Ed25519 密钥签名、公钥必须在受信发布者名单内——
// 目录可信 ≠ 包可信：每个包下载后仍走各自的 VerifyTrusted，两道闸互不替代。
// 目录里条目的身份字段（pack_id/kind/regions/since/until/doc_count）取自包内已签名的
// manifest，目录不得自行改写（防伪造包身份）。
package pack

import (
	"archive/zip"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/hashutil"
)

var (
	ErrBadCatalog    = errors.New("更新包目录非法")
	ErrDuplicatePack = errors.New("更新包目录中包标识重复")
)

// CatalogPack 目录条目：指向一个 zip 归档；url 为下载直链，sha256/size 锚定归档文件
// 本身的完整性（下载后先对哈希，再解包验签）。
type CatalogPack struct {
	PackID   string   `json:"pack_id"`
	Kind     string   `json:"kind"` // base|delta
	Regions  []string `json:"regions,omitempty"`
	Since    string   `json:"since,omitempty"`
	Until    string   `json:"until,omitempty"`
	DocCount int      `json:"doc_count,omitempty"`
	URL      string   `json:"url"`
	Size     int64    `json:"size"`
	SHA256   string   `json:"sha256"`
}

// Catalog index.json 的结构。PubKeyHex 在签名体内：公钥与目录内容同批被签名锁定
// （与包内 pubkey.hex + manifest.sig 同一模型）。
type Catalog struct {
	GeneratedAt string        `json:"generated_at"`
	ToolVersion string        `json:"tool_version"`
	PubKeyHex   string        `json:"pubkey"`
	Packs       []CatalogPack `json:"packs"`
}

var catalogSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ReadPackMeta 从 zip 归档读取包身份（来自包内已签名 manifest）并计算归档文件的
// sha256/size。无 pack_id 或 kind 非法的旧包不得进目录（须以 -pack-id/-kind 重打包）。
func ReadPackMeta(zipPath, url string) (CatalogPack, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return CatalogPack{}, fmt.Errorf("更新包归档不可读: %w", err)
	}
	defer zr.Close()
	var body []byte
	for _, zf := range zr.File {
		if filepath.ToSlash(zf.Name) == "manifest.json" {
			rc, err := zf.Open()
			if err != nil {
				return CatalogPack{}, err
			}
			body, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return CatalogPack{}, err
			}
			break
		}
	}
	if body == nil {
		return CatalogPack{}, fmt.Errorf("%w: 归档缺少 manifest.json", ErrManifestMissing)
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return CatalogPack{}, fmt.Errorf("%w: manifest 不可解析", ErrSignatureFail)
	}
	if m.PackID == "" {
		return CatalogPack{}, fmt.Errorf("%w: %s 无 pack_id（旧整包不得进目录，请用 -pack-id/-kind 重新打包）", ErrBadCatalog, filepath.Base(zipPath))
	}
	if m.Kind != "base" && m.Kind != "delta" {
		return CatalogPack{}, fmt.Errorf("%w: %s kind=%q（目录仅收 base/delta）", ErrBadCatalog, m.PackID, m.Kind)
	}
	sha, err := hashutil.SHA256File(zipPath)
	if err != nil {
		return CatalogPack{}, err
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		return CatalogPack{}, err
	}
	if url == "" {
		return CatalogPack{}, fmt.Errorf("%w: %s 缺少下载 URL", ErrBadCatalog, m.PackID)
	}
	return CatalogPack{
		PackID: m.PackID, Kind: m.Kind, Regions: m.Regions,
		Since: m.Since, Until: m.Until, DocCount: m.DocCount,
		URL: url, Size: st.Size(), SHA256: sha,
	}, nil
}

// BuildCatalog 生成并签名目录：index.json + index.sig 写入 outDir。
// 条目按 pack_id 排序（可复现）；非法条目（缺 id/哈希、kind 不符、重复 id）拒绝出包。
func BuildCatalog(packs []CatalogPack, outDir, keyPath string) error {
	if len(packs) == 0 {
		return fmt.Errorf("%w: 目录为空", ErrBadCatalog)
	}
	seen := map[string]bool{}
	for _, p := range packs {
		if p.PackID == "" || (p.Kind != "base" && p.Kind != "delta") {
			return fmt.Errorf("%w: 条目 %q kind=%q", ErrBadCatalog, p.PackID, p.Kind)
		}
		if !catalogSHA.MatchString(p.SHA256) {
			return fmt.Errorf("%w: %s 的归档 sha256 缺失或非法", ErrBadCatalog, p.PackID)
		}
		if !strings.HasPrefix(p.URL, "https://") && !strings.HasPrefix(p.URL, "http://") {
			return fmt.Errorf("%w: %s 的下载 URL 非法: %q", ErrBadCatalog, p.PackID, p.URL)
		}
		if seen[p.PackID] {
			return fmt.Errorf("%w: %s", ErrDuplicatePack, p.PackID)
		}
		seen[p.PackID] = true
	}
	list := append([]CatalogPack(nil), packs...)
	sort.Slice(list, func(i, j int) bool { return list[i].PackID < list[j].PackID })

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("读取签名私钥失败（私钥独立存储，不进包）: %w", err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(keyBytes)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("私钥文件格式错误: %s", keyPath)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	c := Catalog{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ToolVersion: ToolVersion,
		PubKeyHex:   hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
		Packs:       list,
	}
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.json"), body, 0o644); err != nil {
		return err
	}
	sig := ed25519.Sign(priv, body)
	return os.WriteFile(filepath.Join(outDir, "index.sig"),
		[]byte(hex.EncodeToString(sig)), 0o644)
}

// ParseCatalog 读取并校验目录：签名有效（用目录内声明的公钥）→ 公钥命中受信名单 →
// 条目结构合法且无重复。任一失败返回哨兵错误，调用方必须放弃该目录。
func ParseCatalog(dir string) (*Catalog, error) {
	body, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: index.json", ErrBadCatalog)
	}
	sigHex, err := os.ReadFile(filepath.Join(dir, "index.sig"))
	if err != nil {
		return nil, fmt.Errorf("%w: index.sig 缺失", ErrBadCatalog)
	}
	var c Catalog
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, fmt.Errorf("%w: index.json 不可解析", ErrBadCatalog)
	}
	pub, err := hex.DecodeString(strings.TrimSpace(c.PubKeyHex))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: 目录公钥非法", ErrSignatureFail)
	}
	sig, err := hex.DecodeString(strings.TrimSpace(string(sigHex)))
	if err != nil || !ed25519.Verify(pub, body, sig) {
		return nil, fmt.Errorf("%w: 目录签名验证失败", ErrSignatureFail)
	}
	if _, err := TrustPubkey(strings.ToLower(strings.TrimSpace(c.PubKeyHex))); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range c.Packs {
		if p.PackID == "" || (p.Kind != "base" && p.Kind != "delta") {
			return nil, fmt.Errorf("%w: 条目 %q kind=%q", ErrBadCatalog, p.PackID, p.Kind)
		}
		if !catalogSHA.MatchString(p.SHA256) {
			return nil, fmt.Errorf("%w: %s 的 sha256 非法", ErrBadCatalog, p.PackID)
		}
		if !strings.HasPrefix(p.URL, "https://") && !strings.HasPrefix(p.URL, "http://") {
			return nil, fmt.Errorf("%w: %s 的 URL 非法", ErrBadCatalog, p.PackID)
		}
		if seen[p.PackID] {
			return nil, fmt.Errorf("%w: %s", ErrDuplicatePack, p.PackID)
		}
		seen[p.PackID] = true
	}
	return &c, nil
}
