// Package pack 离线更新包（开发文档 6.4、16.2、FR-06）。
// 包结构：manifest.json + manifest.sig + pubkey.hex + pdf/ md/ provenance/ changes.csv。
// 导入时先校验哈希与清单一致性，失败保留旧数据；不做静默自动抓取更新。
package pack

import (
	"crypto/ed25519"
	"crypto/sha256"
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
	"github.com/futuremeng/OpenLaw/validate"
)

var (
	ErrManifestMissing = errors.New("更新包缺少 manifest")
	ErrSignatureFail   = errors.New("更新包签名验证失败")
	ErrFileMismatch    = errors.New("更新包文件与 manifest 不一致")
	ErrExtraFile       = errors.New("更新包含 manifest 未收录的文件")
	ErrBadChange       = errors.New("changes.csv 记录非法")
	ErrBadPackKind     = errors.New("包种类非法（仅支持 base=基础包 / delta=增量包）")
	ErrDocConflict     = errors.New("条目已存在但内容哈希不一致（冲突转人工，禁止覆盖原始层）")
)

// ToolVersion 打包工具版本（16.2）。
const ToolVersion = "openlaw-pack/0.1.0"

// Entry manifest 中的单个文件条目：相对路径、大小、SHA-256。
type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest 更新包清单（v2 起携带 license/attribution：欧盟等来源要求重使用署名，
// 签名字段随 manifest.json 一起被签名保护，verify 无需额外改动）。
// 包模型 v2（基础包/增量包分发）：pack_id/kind/regions/since/until/doc_count 均为可选，
// 旧包缺这些字段读为空 = 旧整包（full）；新字段随 manifest.json 整体被签名保护。
type Manifest struct {
	GeneratedAt string   `json:"generated_at"`
	ToolVersion string   `json:"tool_version"`
	License     string   `json:"license,omitempty"`
	Attribution string   `json:"attribution,omitempty"`
	PackID      string   `json:"pack_id,omitempty"`   // 台账主键（同一发布者内唯一）
	Kind        string   `json:"kind,omitempty"`      // base=基础包 | delta=增量包 | 空=旧整包
	Regions     []string `json:"regions,omitempty"`   // 覆盖地域（区划码；CN=国家级；空=全部地域）
	Since       string   `json:"since,omitempty"`     // 增量窗口起（publish_date，含）
	Until       string   `json:"until,omitempty"`     // 增量窗口止（含）
	DocCount    int      `json:"doc_count,omitempty"` // changes.csv 条目数
	Files       []Entry  `json:"files"`
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
	Whole         bool   // 全文即一节模式（article_no=全文，无条文结构文档）
	Lang          string // 正文语言（ISO 639-3；空=zho。EU 通道 eng 等，回灌按语言选切分器）
	RegionCode    string // 区划码（GB/T 2260 风格，与 geo 一致）；空 = 上游未标注
	RegionSource  string // 上游标注来源（auto/manual）；导入侧一律写 pack（本库未复核）
}

var changeHeader = []string{"action", "doc_id", "law_id", "law_name", "short_name", "issuer",
	"effect_level", "version_no", "effective_date", "expire_date", "publish_date", "is_gongbao",
	"source_url", "source_type", "notes", "whole", "lang", "region_code", "region_source"}

// 历史表头（读取时向后兼容，写出只用 changeHeader）：
// v0 = 15 列（无 whole/lang/region）；v1 = 16 列（+whole）；
// v2 = 17 列（+lang）；v3 = 19 列（+region_code/region_source，即当前 changeHeader）。
var (
	oldChangeHeader = changeHeader[:15]
	v1ChangeHeader  = changeHeader[:16]
	v2ChangeHeader  = changeHeader[:17]
)

// Build 从数据目录构建更新包（旧整包语义：全库导出）。行为与历史版本兼容。
func Build(db *store.DB, dataDir, outDir, keyPath, license, attribution string) error {
	return BuildWithOptions(db, dataDir, outDir, keyPath, license, attribution, BuildOptions{})
}

// BuildOptions 包切分选项（基础包按地域 / 增量包按发布日期窗口）。
// 零值 = 旧整包：全库导出，manifest 不带新字段。
type BuildOptions struct {
	PackID  string   // 台账主键（同一发布者内唯一）；空 = 导入侧回退 "pack:"+generated_at
	Kind    string   // base=基础包 | delta=增量包；空=旧整包；其余取值拒绝
	Regions []string // 按 law.region_code 过滤；"CN"=国家级（区划码为空）；多个取并集
	Since   string   // 按 document.publish_date 过滤（含）；空=无下限。publish_date 为空的条目
	Until   string   // 同上为上限（含）。——不落入任何日期窗口（如实处理，不得猜日期）
}

// BuildWithOptions 构建基础包/增量包：先按选项导出 changes.csv，再只复制清单引用到的
// 原始层文件（增量包体积正比于内容，未被引用的文件不进包），最后生成 manifest 并用
// 私钥签名（私钥独立存储，不进包）。license/attribution 为共享合规声明
// （空值用默认：法律文本公有领域 + 转发须署名出处）。
func BuildWithOptions(db *store.DB, dataDir, outDir, keyPath, license, attribution string, opts BuildOptions) error {
	if license == "" {
		license = validate.DefaultLicense
	}
	if attribution == "" {
		attribution = validate.DefaultAttribution
	}
	if opts.Kind != "" && opts.Kind != "base" && opts.Kind != "delta" {
		return fmt.Errorf("%w: %q", ErrBadPackKind, opts.Kind)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	docIDs, count, err := writeChanges(db, filepath.Join(outDir, "changes.csv"), opts)
	if err != nil {
		return err
	}
	for _, sub := range []string{"pdf", "md", "provenance"} {
		src := filepath.Join(dataDir, sub)
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("数据目录缺少 %s: %w", sub, err)
		}
		if err := copyTreeFiltered(src, filepath.Join(outDir, sub), docIDs); err != nil {
			return err
		}
	}
	for _, sub := range []string{"docx", "txt", "xml"} { // 可选原始层（docx 存量/xml EU 通道出现）
		src := filepath.Join(dataDir, sub)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyTreeFiltered(src, filepath.Join(outDir, sub), docIDs); err != nil {
			return err
		}
	}
	return seal(outDir, keyPath, license, attribution, opts, count)
}

// writeChanges 从数据库导出变更清单（当前全部为 add；失效/修改关系不自动判定，10 章）。
// opts 的地域/日期过滤实现基础包按地域切分与增量包按窗口切分：
//   - Regions：law.region_code 前缀匹配（"CN" = 区划码为空的国家级条目）；
//   - Since/Until：document.publish_date 闭区间；publish_date 为空的条目在设了窗口时
//     一律不入包（上游未标注就是未标注，不得猜）。
//
// 返回清单覆盖的 doc_id 集合（原始层按此复制）与条目数。
func writeChanges(db *store.DB, path string, opts BuildOptions) (map[string]bool, int, error) {
	query := `SELECT d.doc_id, l.law_id, l.name, COALESCE(l.short_name,''),
		COALESCE(l.issuer,''), COALESCE(l.effect_level,''), v.version_no, v.effective_date,
		COALESCE(v.expire_date,''), COALESCE(d.publish_date,''), d.is_gongbao,
		COALESCE(d.source_url,''), COALESCE(d.source_type,''),
		EXISTS(SELECT 1 FROM article a WHERE a.version_id = v.version_id AND a.article_no = '全文'),
		COALESCE(v.lang,'zho'), COALESCE(l.region_code,''), COALESCE(l.region_source,'')
		FROM law_version v JOIN law l ON l.law_id = v.law_id
		JOIN document d ON d.doc_id = v.doc_id`
	where := []string{}
	args := []any{}
	if len(opts.Regions) > 0 {
		ors := make([]string, 0, len(opts.Regions))
		for _, r := range opts.Regions {
			if r == "CN" {
				ors = append(ors, "(l.region_code IS NULL OR l.region_code = '')")
				continue
			}
			ors = append(ors, "l.region_code LIKE ?")
			args = append(args, r+"%")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	if opts.Since != "" {
		where = append(where, "d.publish_date >= ?")
		args = append(args, opts.Since)
	}
	if opts.Until != "" {
		where = append(where, "d.publish_date <= ?")
		args = append(args, opts.Until)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY v.law_id, v.effective_date"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	f, err := os.Create(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(changeHeader); err != nil {
		return nil, 0, err
	}
	docIDs := map[string]bool{}
	count := 0
	for rows.Next() {
		var docID, lawID, name, short, issuer, level, vno, eff, exp, pub, url, stype, lang string
		var regionCode, regionSource string
		var gongbao, whole int
		if err := rows.Scan(&docID, &lawID, &name, &short, &issuer, &level, &vno, &eff,
			&exp, &pub, &gongbao, &url, &stype, &whole, &lang, &regionCode, &regionSource); err != nil {
			return nil, 0, err
		}
		rec := []string{"add", docID, lawID, name, short, issuer, level, vno, eff, exp, pub,
			strconv.Itoa(gongbao), url, stype, "", strconv.Itoa(whole), lang, regionCode, regionSource}
		if len(rec) != len(changeHeader) {
			return nil, 0, fmt.Errorf("%w: %s", ErrBadChange, docID)
		}
		if err := w.Write(rec); err != nil {
			return nil, 0, err
		}
		docIDs[docID] = true
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	w.Flush()
	if count == 0 {
		return nil, 0, fmt.Errorf("没有符合条件的条目，无变更可打包（检查地域/日期过滤条件）")
	}
	return docIDs, count, w.Error()
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
	if len(records) < 2 {
		return nil, fmt.Errorf("%w: 表头不符", ErrBadChange)
	}
	// 表头版本：v3（当前，含 region 两列）/ v2（+lang）/ v1（+whole）/ v0。
	// 旧包照常读取，缺的列一律留空——不得用默认值伪造（region 留空就是「上游未标注」）。
	version := 3
	switch strings.Join(records[0], ",") {
	case strings.Join(changeHeader, ","):
	case strings.Join(v2ChangeHeader, ","):
		version = 2
	case strings.Join(v1ChangeHeader, ","):
		version = 1
	case strings.Join(oldChangeHeader, ","):
		version = 0
	default:
		return nil, fmt.Errorf("%w: 表头不符", ErrBadChange)
	}
	wantCols := len(oldChangeHeader)
	if version >= 1 {
		wantCols = len(v1ChangeHeader)
	}
	if version >= 2 {
		wantCols = len(v2ChangeHeader)
	}
	if version >= 3 {
		wantCols = len(changeHeader)
	}
	out := make([]Change, 0, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) != wantCols {
			return nil, fmt.Errorf("%w: 列数不符 %v", ErrBadChange, rec)
		}
		g, _ := strconv.Atoi(rec[11])
		c := Change{
			Action: rec[0], DocID: rec[1], LawID: rec[2], LawName: rec[3], ShortName: rec[4],
			Issuer: rec[5], EffectLevel: rec[6], VersionNo: rec[7], EffectiveDate: rec[8],
			ExpireDate: rec[9], PublishDate: rec[10], IsGongbao: g == 1,
			SourceURL: rec[12], SourceType: rec[13], Notes: rec[14],
		}
		if version >= 1 {
			c.Whole = rec[15] == "1"
		}
		if version >= 2 {
			c.Lang = rec[16]
		}
		if version >= 3 {
			c.RegionCode, c.RegionSource = rec[17], rec[18]
		}
		out = append(out, c)
	}
	return out, nil
}

// seal 扫描包内全部文件（除 manifest.json/manifest.sig/pubkey.hex）生成清单并签名。
// opts/docCount 写入包身份字段（pack_id/kind/regions/since/until/doc_count）——
// 它们随 manifest.json 整体被签名保护，篡改即验签失败。
func seal(packDir, keyPath, license, attribution string, opts BuildOptions, docCount int) error {
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

	m := Manifest{GeneratedAt: time.Now().UTC().Format(time.RFC3339), ToolVersion: ToolVersion,
		License: license, Attribution: attribution,
		PackID: opts.PackID, Kind: opts.Kind, Regions: opts.Regions,
		Since: opts.Since, Until: opts.Until, DocCount: docCount,
		Files: entries}
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

// ImportReport 包级导入结果（台账 + 诚实汇报：导入/跳过/已装过 分列，不混为一谈）。
type ImportReport struct {
	PackID         string // 台账主键（manifest.pack_id；旧整包回退 "pack:"+generated_at）
	Kind           string // base|delta|full（manifest 无 kind = 旧整包）
	Regions        []string
	AlreadyApplied bool // true = 台账已有 status=applied，本次整包跳过
	Total          int  // changes.csv 条目数
	Imported       int  // 本次实际导入
	Skipped        int  // 库内已存在且哈希一致而跳过的条目
	Since          string
	Until          string
	Results        []ingest.Result
}

// Import 校验通过后导入目标数据目录（6.4）。返回实际导入的条目结果。
func Import(db *store.DB, targetDataDir, packDir string) ([]ingest.Result, error) {
	rep, err := ImportPack(db, targetDataDir, packDir)
	return rep.Results, err
}

// ImportPack 校验通过后导入（6.4），返回包级结果供 UI/CLI 如实汇报。
// 先 VerifyTrusted：签名有效 + 哈希一致 + 无多余文件 + 公钥命中受信发布者名单，
// 任一失败直接拒绝，不触碰现有数据（来源不可信的包等同损坏包）。
// 幂等（pack_log 台账 + 逐条哈希比对）：
//   - 台账 status=applied 的包整包跳过（AlreadyApplied=true，不重跑）；
//   - doc_id 已存在且原始层哈希一致 → 跳过（增量包重放/包重叠可安全续装）；
//   - 哈希不一致 = 冲突 → 中止转人工（ErrDocConflict，禁止覆盖原始层）。
//
// 中途失败：已导入条目为纯新增可追溯（import_log），台账记 partial，旧数据不受影响；
// 返回错误由人工处置。逐条走与 openlaw-ingest 完全相同的导入闭环。
func ImportPack(db *store.DB, targetDataDir, packDir string) (ImportReport, error) {
	if err := VerifyTrusted(packDir); err != nil {
		return ImportReport{}, fmt.Errorf("更新包校验失败，拒绝导入（保留旧数据）: %w", err)
	}
	return importVerified(db, targetDataDir, packDir, true)
}

// ImportAllowUntrusted 仅做完整性校验（Verify），不查受信名单。
// 供本地自验与下游已自行装配信任的场景；用于分发即放弃来源验证，慎用。
func ImportAllowUntrusted(db *store.DB, targetDataDir, packDir string) ([]ingest.Result, error) {
	rep, err := ImportPackAllowUntrusted(db, targetDataDir, packDir)
	return rep.Results, err
}

// ImportPackAllowUntrusted 同 ImportPack 但不查受信名单（场景同上，慎用）。
func ImportPackAllowUntrusted(db *store.DB, targetDataDir, packDir string) (ImportReport, error) {
	if err := Verify(packDir); err != nil {
		return ImportReport{}, fmt.Errorf("更新包校验失败，拒绝导入（保留旧数据）: %w", err)
	}
	return importVerified(db, targetDataDir, packDir, false)
}

func importVerified(db *store.DB, targetDataDir, packDir string, trusted bool) (ImportReport, error) {
	var rep ImportReport
	body, err := os.ReadFile(filepath.Join(packDir, "manifest.json"))
	if err != nil {
		return rep, ErrManifestMissing
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return rep, ErrSignatureFail
	}
	rep.PackID = packLedgerID(m, packDir)
	rep.Kind = m.Kind
	if rep.Kind == "" {
		rep.Kind = "full"
	}
	rep.Regions = m.Regions
	rep.Since, rep.Until = m.Since, m.Until
	keyID := ""
	if trusted {
		if keyID, err = TrustOf(packDir); err != nil {
			return rep, err
		}
	}
	// 台账幂等：已装完的包整包跳过，不重跑、不重复写审计。
	var status string
	if err := db.QueryRow(`SELECT status FROM pack_log WHERE pack_id = ?`, rep.PackID).Scan(&status); err == nil && status == "applied" {
		rep.AlreadyApplied = true
		return rep, nil
	}
	changes, err := ReadChanges(packDir)
	if err != nil {
		return rep, err
	}
	rep.Total = len(changes)
	im := &ingest.Importer{DataDir: targetDataDir, DB: db}
	batchID := rep.PackID // 地域携带写入的审计关联号：必须能追到具体包
	for i, c := range changes {
		if c.Action != "add" {
			_ = writePackLog(db, rep, keyID, "partial")
			return rep, fmt.Errorf("%w: 第 %d 行 action=%q（当前仅支持 add，失效/废止关系须人工确认）",
				ErrBadChange, i+2, c.Action)
		}
		srcFile := ""
		for _, cand := range []string{
			filepath.Join(packDir, "pdf", c.DocID+".pdf"),
			filepath.Join(packDir, "docx", c.DocID+".docx"),
			filepath.Join(packDir, "txt", c.DocID+".txt"),
			filepath.Join(packDir, "xml", c.DocID+".xml"),
		} {
			if _, err := os.Stat(cand); err == nil {
				srcFile = cand
				break
			}
		}
		if srcFile == "" {
			_ = writePackLog(db, rep, keyID, "partial")
			return rep, fmt.Errorf("包内缺少 %s 的原始文件（pdf/docx/txt/xml 均无）", c.DocID)
		}
		md := filepath.Join(packDir, "md", c.DocID+".md")
		for _, f := range []string{md, filepath.Join(packDir, "provenance", c.DocID+".json")} {
			if _, err := os.Stat(f); err != nil {
				_ = writePackLog(db, rep, keyID, "partial")
				return rep, fmt.Errorf("包内缺少 %s: %w", filepath.Base(f), err)
			}
		}
		// 重复条目：与库内原始层哈希一致 → 跳过（增量包重放/包重叠可安全续装）；
		// 不一致 → 冲突中止转人工，禁止覆盖原始层。
		var stored string
		if err := db.QueryRow(`SELECT sha256 FROM document WHERE doc_id = ?`, c.DocID).Scan(&stored); err == nil {
			sha, herr := hashutil.SHA256File(srcFile)
			if herr != nil {
				_ = writePackLog(db, rep, keyID, "partial")
				return rep, herr
			}
			if sha == stored {
				rep.Skipped++
				continue
			}
			_ = writePackLog(db, rep, keyID, "partial")
			return rep, fmt.Errorf("%w: %s", ErrDocConflict, c.DocID)
		}
		res, err := im.Import(ingest.Request{
			DocID: c.DocID, LawID: c.LawID, LawName: c.LawName, ShortName: c.ShortName,
			Issuer: c.Issuer, EffectLevel: c.EffectLevel, VersionNo: c.VersionNo,
			EffectiveDate: c.EffectiveDate, ExpireDate: c.ExpireDate,
			SourceURL: c.SourceURL, SourceType: c.SourceType, IsGongbao: c.IsGongbao,
			PublishDate: c.PublishDate, ConfirmStatus: "pending", Notes: c.Notes,
			PDFPath: srcFile, MDPath: md, WholeTextMode: c.Whole, Lang: c.Lang,
			RegionCode: c.RegionCode, RegionSource: c.RegionSource, RegionBatch: batchID,
		})
		if err != nil {
			_ = writePackLog(db, rep, keyID, "partial")
			return rep, fmt.Errorf("导入 %s 失败（第 %d/%d 条）: %w", c.DocID, i+1, len(changes), err)
		}
		rep.Imported++
		rep.Results = append(rep.Results, *res)
	}
	if err := writePackLog(db, rep, keyID, "applied"); err != nil {
		return rep, fmt.Errorf("条目已全部导入，但安装台账写入失败（下次导入将按条目哈希幂等续跑）: %w", err)
	}
	return rep, nil
}

// packLedgerID 包台账主键：优先 manifest.pack_id；旧包无此字段时回退
// "pack:"+generated_at（读不到退回包目录名）并附加内容指纹——generated_at 只到秒，
// 同秒产出的两个不同包若只取时间会互相误判「已装过」；指纹取文件哈希清单的
// SHA-256 前 12 位，保证同一包可重放、不同包不误判。审计必须能追到具体包。
func packLedgerID(m Manifest, packDir string) string {
	if m.PackID != "" {
		return m.PackID
	}
	base := "pack:" + m.GeneratedAt
	if m.GeneratedAt == "" {
		base = "pack:" + filepath.Base(packDir)
	}
	items := make([]string, 0, len(m.Files))
	for _, e := range m.Files {
		items = append(items, e.Path+" "+e.SHA256)
	}
	sort.Strings(items)
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return base + "-" + hex.EncodeToString(sum[:])[:12]
}

// writePackLog 写 pack_log 安装台账（按 pack_id upsert，保留最近一次计数与状态）。
// status=applied 是「已装完」的唯一依据；partial 表示中断/冲突中止，重放时逐条幂等续跑。
func writePackLog(db *store.DB, rep ImportReport, keyID, status string) error {
	_, err := db.Exec(`INSERT INTO pack_log(pack_id, kind, regions, since, until, key_id, total, imported, skipped, status, applied_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(pack_id) DO UPDATE SET
			kind=excluded.kind, regions=excluded.regions, since=excluded.since, until=excluded.until,
			key_id=excluded.key_id, total=excluded.total, imported=excluded.imported,
			skipped=excluded.skipped, status=excluded.status, applied_at=excluded.applied_at`,
		rep.PackID, rep.Kind, strings.Join(rep.Regions, ","), rep.Since, rep.Until, keyID,
		rep.Total, rep.Imported, rep.Skipped, status,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// copyTreeFiltered 复制子树，只保留 changes.csv 引用到的条目文件
// （按文件名主干 doc_id 匹配；docIDs 为 nil = 全部复制）。
// 未被清单引用的文件不进包：包体与清单严格一致，增量包体积正比于内容。
func copyTreeFiltered(src, dst string, docIDs map[string]bool) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if info.Name() == ".gitkeep" {
			return nil
		}
		if docIDs != nil {
			stem := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
			if !docIDs[stem] {
				return nil
			}
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
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
