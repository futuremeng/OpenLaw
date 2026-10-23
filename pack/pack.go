// Package pack 离线更新包（开发文档 6.4、16.2、FR-06）。
// 包结构：manifest.json + manifest.sig + pubkey.hex + pdf/ md/ provenance/ changes.csv。
// 导入时先校验哈希与清单一致性，失败保留旧数据；不做静默自动抓取更新。
package pack

import (
	"crypto/ed25519"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/hashutil"
	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/store"
)

var (
	ErrManifestMissing = errors.New("更新包缺少 manifest")
	ErrSignatureFail   = errors.New("更新包签名验证失败")
	ErrFileMismatch    = errors.New("更新包文件与 manifest 不一致")
	ErrExtraFile       = errors.New("更新包含 manifest 未收录的文件")
	ErrBadChange       = errors.New("changes.csv 记录非法")
)

// ToolVersion 打包工具版本（16.2）。
const ToolVersion = "openlaw-pack/0.1.0"

// Entry manifest 中的单个文件条目：相对路径、大小、SHA-256。
type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest 更新包清单。
type Manifest struct {
	GeneratedAt string  `json:"generated_at"`
	ToolVersion string  `json:"tool_version"`
	Files       []Entry `json:"files"`
}

// Change changes.csv 的一条变更（action 目前仅支持 add）。
type Change struct {
	Action        string
	DocID         string
	LawID         string
	LawName       string
	ShortName     string
	Issuer        string
	EffectLevel   string
	VersionNo     string
	EffectiveDate string
	ExpireDate    string
	PublishDate   string
	IsGongbao     bool
	SourceURL     string
	SourceType    string
	Notes         string
}

var changeHeader = []string{"action", "doc_id", "law_id", "law_name", "short_name", "issuer",
	"effect_level", "version_no", "effective_date", "expire_date", "publish_date", "is_gongbao",
	"source_url", "source_type", "notes"}

// Build 从数据目录构建更新包：复制 pdf/ md/ provenance/，从数据库导出 changes.csv，
// 生成 manifest.json 并用私钥签名（私钥独立存储，不进包）。
func Build(db *store.DB, dataDir, outDir, keyPath string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, sub := range []string{"pdf", "md", "provenance"} {
		src := filepath.Join(dataDir, sub)
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("数据目录缺少 %s: %w", sub, err)
		}
		if err := copyTree(src, filepath.Join(outDir, sub)); err != nil {
			return err
		}
	}
	if err := writeChanges(db, filepath.Join(outDir, "changes.csv")); err != nil {
		return err
	}
	return seal(outDir, keyPath)
}

// writeChanges 从数据库导出变更清单（当前全部为 add；失效/修改关系不自动判定，10 章）。
func writeChanges(db *store.DB, path string) error {
	rows, err := db.Query(`SELECT d.doc_id, l.law_id, l.name, COALESCE(l.short_name,''),
		COALESCE(l.issuer,''), COALESCE(l.effect_level,''), v.version_no, v.effective_date,
		COALESCE(v.expire_date,''), COALESCE(d.publish_date,''), d.is_gongbao,
		COALESCE(d.source_url,''), COALESCE(d.source_type,'')
		FROM law_version v JOIN law l ON l.law_id = v.law_id
		JOIN document d ON d.doc_id = v.doc_id
		ORDER BY v.law_id, v.effective_date`)
	if err != nil {
		return err
	}
	defer rows.Close()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(changeHeader); err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var docID, lawID, name, short, issuer, level, vno, eff, exp, pub, url, stype string
		var gongbao int
		if err := rows.Scan(&docID, &lawID, &name, &short, &issuer, &level, &vno, &eff,
			&exp, &pub, &gongbao, &url, &stype); err != nil {
			return err
		}
		rec := []string{"add", docID, lawID, name, short, issuer, level, vno, eff, exp, pub,
			strconv.Itoa(gongbao), url, stype, ""}
		if len(rec) != len(changeHeader) {
			return fmt.Errorf("%w: %s", ErrBadChange, docID)
		}
		if err := w.Write(rec); err != nil {
			return err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	w.Flush()
	if count == 0 {
		return fmt.Errorf("数据库为空，无变更可打包")
	}
	return w.Error()
}

// ReadChanges 解析 changes.csv。
func ReadChanges(packDir string) ([]Change, error) {
	f, err := os.Open(filepath.Join(packDir, "changes.csv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 || strings.Join(records[0], ",") != strings.Join(changeHeader, ",") {
		return nil, fmt.Errorf("%w: 表头不符", ErrBadChange)
	}
	out := make([]Change, 0, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) != len(changeHeader) {
			return nil, fmt.Errorf("%w: 列数不符 %v", ErrBadChange, rec)
		}
		g, _ := strconv.Atoi(rec[11])
		out = append(out, Change{
			Action: rec[0], DocID: rec[1], LawID: rec[2], LawName: rec[3], ShortName: rec[4],
			Issuer: rec[5], EffectLevel: rec[6], VersionNo: rec[7], EffectiveDate: rec[8],
			ExpireDate: rec[9], PublishDate: rec[10], IsGongbao: g == 1,
			SourceURL: rec[12], SourceType: rec[13], Notes: rec[14],
		})
	}
	return out, nil
}

// seal 扫描包内全部文件（除 manifest.json/manifest.sig/pubkey.hex）生成清单并签名。
func seal(packDir, keyPath string) error {
	entries := []Entry{}
	err := filepath.Walk(packDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel := filepath.Base(p)
		if rel == "manifest.json" || rel == "manifest.sig" || rel == "pubkey.hex" {
			return nil
		}
		sha, herr := hashutil.SHA256File(p)
		if herr != nil {
			return herr
		}
		r, rerr := filepath.Rel(packDir, p)
		if rerr != nil {
			return rerr
		}
		entries = append(entries, Entry{Path: filepath.ToSlash(r), Size: info.Size(), SHA256: sha})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	m := Manifest{GeneratedAt: time.Now().UTC().Format(time.RFC3339), ToolVersion: ToolVersion, Files: entries}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("读取签名私钥失败（私钥独立存储，不进包）: %w", err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(keyBytes)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("私钥文件格式错误: %s", keyPath)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	sig := ed25519.Sign(priv, body)

	if err := os.WriteFile(filepath.Join(packDir, "manifest.json"), body, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packDir, "manifest.sig"),
		[]byte(hex.EncodeToString(sig)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(packDir, "pubkey.hex"),
		[]byte(hex.EncodeToString(priv.Public().(ed25519.PublicKey))), 0o644)
}

// Verify 校验更新包：签名有效 → 每个清单条目哈希一致 → 包内无未收录文件。
// 任一失败返回哨兵错误，调用方必须放弃该包（6.4 失败保留旧数据）。
func Verify(packDir string) error {
	body, err := os.ReadFile(filepath.Join(packDir, "manifest.json"))
	if err != nil {
		return ErrManifestMissing
	}
	pubHex, err := os.ReadFile(filepath.Join(packDir, "pubkey.hex"))
	if err != nil {
		return ErrSignatureFail
	}
	pub, err := hex.DecodeString(strings.TrimSpace(string(pubHex)))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ErrSignatureFail
	}
	sigHex, err := os.ReadFile(filepath.Join(packDir, "manifest.sig"))
	if err != nil {
		return ErrSignatureFail
	}
	sig, err := hex.DecodeString(strings.TrimSpace(string(sigHex)))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), body, sig) {
		return ErrSignatureFail
	}

	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return ErrSignatureFail
	}
	listed := map[string]bool{}
	for _, e := range m.Files {
		listed[filepath.ToSlash(e.Path)] = true
		sha, err := hashutil.SHA256File(filepath.Join(packDir, filepath.FromSlash(e.Path)))
		if err != nil || sha != e.SHA256 {
			return fmt.Errorf("%w: %s", ErrFileMismatch, e.Path)
		}
	}
	err = filepath.Walk(packDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel := filepath.Base(p)
		if rel == "manifest.json" || rel == "manifest.sig" || rel == "pubkey.hex" {
			return nil
		}
		r, rerr := filepath.Rel(packDir, p)
		if rerr != nil {
			return rerr
		}
		if !listed[filepath.ToSlash(r)] {
			return fmt.Errorf("%w: %s", ErrExtraFile, r)
		}
		return nil
	})
	return err
}

// Import 校验通过后逐条导入目标数据目录（6.4）。
// 先 Verify：任何哈希/签名/多余文件问题直接拒绝，不触碰现有数据。
// 逐条走与 openlaw-ingest 完全相同的导入闭环（重复 doc_id / 区间重叠会被拦截）。
// 中途失败：已导入条目为纯新增可追溯（import_log），旧数据不受影响；返回错误由人工处置。
func Import(db *store.DB, targetDataDir, packDir string) ([]ingest.Result, error) {
	if err := Verify(packDir); err != nil {
		return nil, fmt.Errorf("更新包校验失败，拒绝导入（保留旧数据）: %w", err)
	}
	changes, err := ReadChanges(packDir)
	if err != nil {
		return nil, err
	}
	im := &ingest.Importer{DataDir: targetDataDir, DB: db}
	var done []ingest.Result
	for i, c := range changes {
		if c.Action != "add" {
			return done, fmt.Errorf("%w: 第 %d 行 action=%q（当前仅支持 add，失效/废止关系须人工确认）",
				ErrBadChange, i+2, c.Action)
		}
		pdf := filepath.Join(packDir, "pdf", c.DocID+".pdf")
		md := filepath.Join(packDir, "md", c.DocID+".md")
		for _, f := range []string{pdf, md, filepath.Join(packDir, "provenance", c.DocID+".json")} {
			if _, err := os.Stat(f); err != nil {
				return done, fmt.Errorf("包内缺少 %s: %w", filepath.Base(f), err)
			}
		}
		res, err := im.Import(ingest.Request{
			DocID: c.DocID, LawID: c.LawID, LawName: c.LawName, ShortName: c.ShortName,
			Issuer: c.Issuer, EffectLevel: c.EffectLevel, VersionNo: c.VersionNo,
			EffectiveDate: c.EffectiveDate, ExpireDate: c.ExpireDate,
			SourceURL: c.SourceURL, SourceType: c.SourceType, IsGongbao: c.IsGongbao,
			PublishDate: c.PublishDate, ConfirmStatus: "pending", Notes: c.Notes,
			PDFPath: pdf, MDPath: md,
		})
		if err != nil {
			return done, fmt.Errorf("导入 %s 失败（第 %d/%d 条）: %w", c.DocID, i+1, len(changes), err)
		}
		done = append(done, *res)
	}
	return done, nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Name() == ".gitkeep" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
