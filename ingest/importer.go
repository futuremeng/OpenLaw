package ingest

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/hashutil"
	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

// Request 一次导入的完整输入（开发文档 6.1）。
type Request struct {
	DocID           string // 文档唯一标识，如 law_company_2023_rev
	LawID           string // 法规标识
	LawName         string
	ShortName       string
	Issuer          string
	EffectLevel     string // 空串按 NULL 入库（受 CHECK 约束）
	VersionNo       string // 版本号，如 2023修正
	EffectiveDate   string // ISO，导入前标准化
	ExpireDate      string // ISO 或空串（未失效）
	SourceURL       string
	SourceType      string
	IsGongbao       bool
	PublishDate     string
	ConfirmStatus   string // confirmed / pending（默认 pending）
	Notes           string
	PDFPath         string // 待导入源文件（PDF/docx/txt）
	MDPath          string // 解析后的 Markdown 正文；留空则按扩展名自动提取
	AutoExpirePrior bool   // 批量场景：自动接续版本链（见 chainPrior）；单文件导入默认关闭
	// EffectiveDateInferred 生效日期为推断值（如正文落款提取）：写入溯源 JSON，
	// 人工确认前不得视为已证事实。
	EffectiveDateInferred bool
	// WholeTextMode 全文即一节：无「第X条」结构的文档（决定/决议/名单类）以单条
	// article（article_no=全文）承载全文入库，可检索且语义显式打标，绝不伪装成条文。
	WholeTextMode bool
	// WholeTextForce 无条件全文即一节：条文标记实为引文的来源（gov.cn 修改/废止
	// 决定正文「第X条」是被引法规条文而非本文件边界）——即使切分器切出条文也强制全文。
	WholeTextForce bool
	// Lang 正文语言（ISO 639-3；空=默认 zho）。EU 通道必填 eng 等——document.lang
	// 与 law_version.lang 写入，且 md 回灌时按语言选切分器（eng→SplitArticlesEU）。
	Lang string
	// Articles 预置条文列表（EU FORMEX 等结构化来源：条文边界来自 XML 结构而非文本
	// 切分）。非空时跳过正文提取与 SplitArticles，直接采用；MDContent 提供 md 层全文。
	Articles []parse.Article
	// MDContent Articles 模式下的 md 层全文（写 data/md/<doc_id>.md，pack 往返用）。
	MDContent string
	// ParserVersion 覆盖 article.parser_version（EU 通道=parse.ParserVersionEU；空=默认）。
	ParserVersion string
}

// Result 导入成功后的产出。
type Result struct {
	DocID       string
	VersionID   string
	SHA256      string
	ArticleIDs  []string
	WholeText   bool             // 全文即一节模式生效（无条文标记文档）
	ChainIssues []validate.Issue // 导入后立即执行的断链检测结果（6.1），空表示链路完好
}

// Importer 导入器：哈希 → 溯源文件 → 解析拆分 → 事务入库 → 日志 → 断链检测。
// 任一环节失败整批回滚，旧数据不受影响。
type Importer struct {
	DataDir string
	DB      *store.DB
}

// Import 执行完整导入流程。
func (im *Importer) Import(req Request) (*Result, error) {
	if err := im.validateRequest(&req); err != nil {
		im.logImport(req, "failed", err.Error())
		return nil, err
	}

	// 1) 哈希（SHA-256 防篡改；MD5 仅判下载损坏）
	sha, md5Hex, size, err := hashutil.HashFile(req.PDFPath)
	if err != nil {
		im.logImport(req, "failed", err.Error())
		return nil, err
	}

	// 2) 解析正文、拆分条文（空结果视为失败，禁止静默入库）
	// Articles 预置模式（EU FORMEX 等结构化来源）：条文边界来自 XML 结构，跳过文本切分。
	// MDPath 留空时按源文件扩展名自动提取：
	//   .pdf  文本层提取（扫描件报错转 OCR/人工）
	//   .docx 正文提取（flk 批量下载存量主格式；.doc 遗留格式报错转 textutil 转换）
	//   .txt/.md 直接读取（人工保存的在线全文等）
	var mdBytes []byte
	var arts []parse.Article
	if len(req.Articles) > 0 {
		arts = req.Articles
		mdBytes = []byte(req.MDContent)
		if len(mdBytes) == 0 {
			var b strings.Builder
			for _, a := range arts {
				b.WriteString(a.Content + "\n\n")
			}
			mdBytes = []byte(strings.TrimSpace(b.String()))
		}
	} else if req.MDPath != "" {
		mdBytes, err = os.ReadFile(req.MDPath)
		if err != nil {
			im.logImport(req, "failed", fmt.Sprintf("读取正文失败: %v", err))
			return nil, fmt.Errorf("读取正文失败: %w", err)
		}
	} else {
		text, xerr := extractByExt(req.PDFPath)
		if xerr != nil {
			im.logImport(req, "failed", xerr.Error())
			return nil, xerr
		}
		mdBytes = []byte(parse.NormalizePDFText(text))
	}
	if arts == nil {
		if req.Lang != "" && req.Lang != "zho" {
			arts = parse.SplitArticlesEU(string(mdBytes)) // 非中文正文按语言选切分器（EU 通道）
		} else {
			arts = parse.SplitArticles(string(mdBytes))
		}
	}
	wholeText := false
	if req.WholeTextForce || len(arts) == 0 {
		if !req.WholeTextMode && !req.WholeTextForce {
			err := fmt.Errorf("未解析出任何条文（检查「第X条」标记；无条文结构文档可用全文模式）: %s", req.MDPath)
			im.logImport(req, "failed", err.Error())
			return nil, err
		}
		// 全文即一节：单条 article 承载全文，parser_version 打 +whole 标记。
		// WholeTextForce：条文标记实为引文的结构化来源（如 gov.cn 修改/废止决定
		// 正文「第一条」均为被引法规条文而非本文件边界）——无条件全文，防误拆。
		wholeText = true
		arts = []parse.Article{{No: parse.WholeArticleNo, Content: string(mdBytes)}}
	}

	// 3) 文件落位：<kind>/<doc_id>.<ext>（pdf/docx/txt…按真实格式；原始层只读，重复 doc_id 拒绝）
	ext, kind := srcExtKind(req.PDFPath)
	dstSrc := filepath.Join(im.DataDir, kind, req.DocID+ext)
	dstMD := filepath.Join(im.DataDir, "md", req.DocID+".md")
	orphanReplaced := false
	if _, err := os.Stat(dstSrc); err == nil {
		var n int
		if qerr := im.DB.QueryRow(`SELECT COUNT(*) FROM document WHERE doc_id = ?`, req.DocID).Scan(&n); qerr != nil {
			im.logImport(req, "failed", qerr.Error())
			return nil, qerr
		}
		if n > 0 {
			err := fmt.Errorf("doc_id 已存在，禁止覆盖原始层: %s", req.DocID)
			im.logImport(req, "failed", err.Error())
			return nil, err
		}
		// 库内无行 = 此前失败运行事务回滚后遗留的孤儿层 → 允许覆盖替换（重跑可恢复），成功日志中留痕
		orphanReplaced = true
	}
	if err := copyFile(req.PDFPath, dstSrc); err != nil {
		im.logImport(req, "failed", err.Error())
		return nil, err
	}
	if req.MDPath != "" {
		if err := copyFile(req.MDPath, dstMD); err != nil {
			im.logImport(req, "failed", err.Error())
			return nil, err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(dstMD), 0o755); err != nil {
			im.logImport(req, "failed", err.Error())
			return nil, err
		}
		if err := os.WriteFile(dstMD, mdBytes, 0o644); err != nil {
			im.logImport(req, "failed", err.Error())
			return nil, err
		}
	}

	// 4) 溯源 JSON（必填字段齐全才允许继续）
	now := time.Now().UTC().Format(time.RFC3339)
	prov := &Provenance{
		DocID: req.DocID, FileName: filepath.Base(dstSrc), FileSize: size,
		SHA256: sha, HashAlgoUsed: "SHA-256", HashComputedAt: now,
		HashComputedBy: "openlaw-ingest/" + versionTag,
		SourceURL:      req.SourceURL, SourceType: req.SourceType, IsGongbao: req.IsGongbao,
		PublishDate: req.PublishDate, EffectiveDate: req.EffectiveDate, ExpireDate: req.ExpireDate,
		EffectiveDateInferred: req.EffectiveDateInferred,
		MDPath:                filepath.Join("md", req.DocID+".md"),
		ConfirmStatus:         req.ConfirmStatus,
		MD5:                   md5Hex, Notes: req.Notes,
	}
	if _, err := prov.Save(im.DataDir); err != nil {
		im.logImport(req, "failed", err.Error())
		return nil, err
	}

	// 5) 事务写入：law / document / law_version / article，任一失败整体回滚
	versionID := req.LawID + "_" + req.VersionNo
	tx, err := im.DB.Begin()
	if err != nil {
		im.logImport(req, "failed", err.Error())
		return nil, err
	}
	articleIDs, chainNote, err := im.writeTx(tx, req, versionID, sha, now, arts, wholeText)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			err = fmt.Errorf("%w（且回滚失败: %v）", err, rbErr)
		}
		im.logImport(req, "rolled_back", err.Error())
		return nil, fmt.Errorf("导入失败已回滚: %w", err)
	}
	if err := tx.Commit(); err != nil {
		im.logImport(req, "rolled_back", err.Error())
		return nil, fmt.Errorf("提交失败: %w", err)
	}
	succReason := ""
	if orphanReplaced {
		succReason = "覆盖孤儿层（此前失败运行回滚遗留，库内无行）"
	}
	if chainNote != "" {
		succReason = strings.TrimSuffix(succReason+"; "+chainNote, "; ")
	}
	if wholeText {
		succReason = strings.TrimSuffix(succReason+"; 全文即一节（无条文结构，whole-text 模式）", "; ")
	}
	if req.EffectiveDateInferred {
		succReason = strings.TrimSuffix(succReason+"; 日期为正文推断（effective_date_inferred）", "; ")
	}
	im.logImport(req, "success", succReason)

	// 6) 触发断链检测（6.1）。断链不抛异常，随结果返回由上层提示。
	res := &Result{
		DocID: req.DocID, VersionID: versionID, SHA256: sha, ArticleIDs: articleIDs,
		WholeText:   wholeText,
		ChainIssues: validate.CheckDocument(im.DB, im.DataDir, req.DocID),
	}
	return res, nil
}

// validateRequest 入参校验与日期标准化（左闭右开区间）。
func (im *Importer) validateRequest(req *Request) error {
	switch {
	case req.DocID == "":
		return fmt.Errorf("缺少 doc_id")
	case req.LawID == "":
		return fmt.Errorf("缺少 law_id")
	case req.LawName == "":
		return fmt.Errorf("缺少法规名称")
	case req.VersionNo == "":
		return fmt.Errorf("缺少版本号")
	case req.PDFPath == "":
		return fmt.Errorf("缺少源文件路径（PDF/docx/txt）")
	}
	if req.ConfirmStatus == "" {
		req.ConfirmStatus = "pending"
	}
	eff, err := parse.NormalizeDate(req.EffectiveDate)
	if err != nil {
		return fmt.Errorf("生效日期: %w", err)
	}
	req.EffectiveDate = eff
	if req.ExpireDate != "" {
		exp, err := parse.NormalizeDate(req.ExpireDate)
		if err != nil {
			return fmt.Errorf("失效日期: %w", err)
		}
		req.ExpireDate = exp
		if exp <= eff {
			return fmt.Errorf("失效日期 %s 必须晚于生效日期 %s（左闭右开区间）", exp, eff)
		}
	}
	return nil
}

// writeTx 在事务内写入全部实体。
func (im *Importer) writeTx(tx *sql.Tx, req Request, versionID, sha, now string, arts []parse.Article, wholeText bool) ([]string, string, error) {
	chainNote := ""
	if req.AutoExpirePrior {
		closed, rechained, cerr := chainPrior(tx, &req)
		if cerr != nil {
			return nil, "", cerr
		}
		if closed+rechained > 0 {
			chainNote = fmt.Sprintf("自动接续版本链: %d 个既有开放版本已闭合", closed)
			if rechained > 0 {
				chainNote += fmt.Sprintf("，%d 个既有闭合集区间前移（新版落于其区间内）", rechained)
			}
		}
	}
	// 区间重叠检查（13.3）：必须在事务内做，防止同日双版本命中
	if err := checkOverlap(tx, req.LawID, req.EffectiveDate, req.ExpireDate); err != nil {
		return nil, "", err
	}

	if _, err := tx.Exec(`INSERT INTO law(law_id, name, short_name, issuer, effect_level, is_local)
		VALUES(?,?,?,?,?,0)
		ON CONFLICT(law_id) DO UPDATE SET name=excluded.name, short_name=excluded.short_name,
			issuer=excluded.issuer, effect_level=excluded.effect_level`,
		req.LawID, req.LawName, nullable(req.ShortName), nullable(req.Issuer), nullable(req.EffectLevel)); err != nil {
		return nil, "", fmt.Errorf("写入 law 失败: %w", err)
	}

	ext, kind := srcExtKind(req.PDFPath)
	lang := req.Lang
	if lang == "" {
		lang = "zho"
	}
	if _, err := tx.Exec(`INSERT INTO document(doc_id, file_name, file_path, file_size, sha256,
		hash_algo, hash_computed_at, source_url, source_type, is_gongbao, publish_date, confirm_status, lang)
		VALUES(?,?,?,?,?,'SHA-256',?,?,?,?,?,?,?)`,
		req.DocID, filepath.Base(req.PDFPath), filepath.Join(kind, req.DocID+ext),
		sizeOrZero(req.PDFPath), sha, now, nullable(req.SourceURL), nullable(req.SourceType),
		boolInt(req.IsGongbao), nullable(req.PublishDate), req.ConfirmStatus, lang); err != nil {
		return nil, "", fmt.Errorf("写入 document 失败: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO law_version(version_id, law_id, version_no, effective_date,
		expire_date, doc_id, is_gongbao, lang) VALUES(?,?,?,?,?,?,?,?)`,
		versionID, req.LawID, req.VersionNo, req.EffectiveDate, nullable(req.ExpireDate),
		req.DocID, boolInt(req.IsGongbao), lang); err != nil {
		return nil, "", fmt.Errorf("写入 law_version 失败: %w", err)
	}

	mdPath := filepath.Join("md", req.DocID+".md")
	pv := parse.ParserVersion
	if req.ParserVersion != "" {
		pv = req.ParserVersion
	} else if wholeText {
		pv = parse.ParserVersionWhole
	}
	ids := make([]string, 0, len(arts))
	for _, a := range arts {
		articleID := versionID + "_" + a.No
		if _, err := tx.Exec(`INSERT INTO article(article_id, version_id, article_no, content,
			md_path, doc_id, parser_version) VALUES(?,?,?,?,?,?,?)`,
			articleID, versionID, a.No, a.Content, mdPath, req.DocID, pv); err != nil {
			return nil, "", fmt.Errorf("写入条文 %s 失败: %w", a.No, err)
		}
		ids = append(ids, articleID)
	}
	return ids, chainNote, nil
}

// chainPrior 版本链自动接续（批量场景，AutoExpirePrior=true）：
//
//	既有开放版本早于待导入版本 → 其 expire_date 置为待导入生效日（旧版闭合）；
//	既有闭合区间跨过待导入生效日（旧版曾被拉长覆盖新版，如存量分批到达）
//	  → 其 expire_date 前移到待导入生效日（区间重链，仅批量模式，留痕）；
//	既有开放版本晚于待导入版本（回填历史版本）→ 待导入版本 expire 置为其中最近生效日；
//	同生效日双版本 → 真冲突，拒绝。全部调整与插入同事务。
func chainPrior(tx *sql.Tx, req *Request) (int, int, error) {
	rows, err := tx.Query(`SELECT version_id, effective_date, expire_date FROM law_version
		WHERE law_id = ?`, req.LawID)
	if err != nil {
		return 0, 0, err
	}
	type ver struct {
		id     string
		eff    string
		expire string // 空串 = 开放
	}
	var vers []ver
	for rows.Next() {
		var v ver
		var exp any
		if err := rows.Scan(&v.id, &v.eff, &exp); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if exp != nil {
			v.expire, _ = exp.(string)
		}
		vers = append(vers, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	rows.Close()
	if len(vers) == 0 {
		return 0, 0, nil
	}
	closed, rechained := 0, 0
	for _, v := range vers {
		switch {
		case v.eff == req.EffectiveDate:
			return 0, 0, fmt.Errorf("同生效日 %s 已存在版本 %s（真冲突，须人工裁定）", req.EffectiveDate, v.id)
		case v.eff < req.EffectiveDate:
			switch {
			case v.expire == "": // 开放的旧版 → 闭合到待导入生效日
				if _, err := tx.Exec(`UPDATE law_version SET expire_date = ? WHERE version_id = ? AND expire_date IS NULL`,
					req.EffectiveDate, v.id); err != nil {
					return 0, 0, err
				}
				closed++
			case v.expire > req.EffectiveDate: // 闭合集跨过新版 → 区间前移重链
				if _, err := tx.Exec(`UPDATE law_version SET expire_date = ? WHERE version_id = ? AND expire_date = ?`,
					req.EffectiveDate, v.id, v.expire); err != nil {
					return 0, 0, err
				}
				rechained++
			}
		default: // 既有版本更新：待导入为其历史版本 → 闭合到最近更新版本的生效日
			if req.ExpireDate == "" || v.eff < req.ExpireDate {
				req.ExpireDate = v.eff
			}
		}
	}
	return closed, rechained, nil
}

// checkOverlap 在事务内执行版本时效区间重叠检查（左闭右开）。
func checkOverlap(tx *sql.Tx, lawID, effective, expire string) error {
	const far = "9999-12-31"
	rows, err := tx.Query(`SELECT version_id, version_no FROM law_version
		WHERE law_id = ?
		  AND effective_date < COALESCE(NULLIF(?, ''), ?)
		  AND COALESCE(expire_date, ?) > ?`, lawID, expire, far, far, effective)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var vid, vno string
		if err := rows.Scan(&vid, &vno); err != nil {
			return err
		}
		return fmt.Errorf("版本区间重叠: 既有版本 %s(%s) 与待导入区间 [%s,%s) 冲突",
			vid, vno, effective, orOpen(expire))
	}
	return rows.Err()
}

// logImport 写导入日志（独立于业务事务，失败也必须留痕）。
// 注意：import_log.doc_id 有外键约束，document 尚未入库的失败场景下
// doc_id 置 NULL，标识保留在 reason 文本中。
func (im *Importer) logImport(req Request, result, reason string) {
	var n int
	_ = im.DB.QueryRow(`SELECT COUNT(*) FROM document WHERE doc_id = ?`, req.DocID).Scan(&n)
	docVal := any(nil)
	if n > 0 {
		docVal = req.DocID
	} else if req.DocID != "" {
		reason = reason + " (doc_id=" + req.DocID + ")"
	}
	_, _ = im.DB.Exec(`INSERT INTO import_log(doc_id, source_url, file_path, file_size, sha256,
		result, reason, created_at) VALUES(?,?,?,?,?,?,?,?)`,
		docVal, nullable(req.SourceURL), nullable(req.PDFPath),
		sizeOrZero(req.PDFPath), nil, result, reason,
		time.Now().UTC().Format(time.RFC3339))
}

const versionTag = "0.1.0"

// srcExtKind 源文件扩展名与存储目录名（无扩展名按 .pdf 兼容旧调用）。
func srcExtKind(path string) (ext, kind string) {
	ext = strings.ToLower(filepath.Ext(path))
	if ext == "" {
		ext = ".pdf"
	}
	return ext, strings.TrimPrefix(ext, ".")
}

// extractByExt 按扩展名自动提取正文。
func extractByExt(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		return parse.ExtractDocxText(path)
	case ".txt", ".md":
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(string(b)) == "" {
			return "", fmt.Errorf("正文文件为空，禁止静默入库")
		}
		return string(b), nil
	case ".doc":
		return "", fmt.Errorf(".doc 遗留二进制格式不支持（OL-1009）：先离线转换 textutil -convert docx %s", path)
	default:
		return parse.ExtractPDFText(path)
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func sizeOrZero(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

func orOpen(expire string) string {
	if expire == "" {
		return "未失效"
	}
	return expire
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
