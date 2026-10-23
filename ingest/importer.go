package ingest

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/futuremeng/OpenLaw/hashutil"
	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

// Request 一次导入的完整输入（开发文档 6.1）。
type Request struct {
	DocID         string // 文档唯一标识，如 law_company_2023_rev
	LawID         string // 法规标识
	LawName       string
	ShortName     string
	Issuer        string
	EffectLevel   string // 空串按 NULL 入库（受 CHECK 约束）
	VersionNo     string // 版本号，如 2023修正
	EffectiveDate string // ISO，导入前标准化
	ExpireDate    string // ISO 或空串（未失效）
	SourceURL     string
	SourceType    string
	IsGongbao     bool
	PublishDate   string
	ConfirmStatus string // confirmed / pending（默认 pending）
	Notes         string
	PDFPath       string // 待导入 PDF
	MDPath        string // 解析后的 Markdown 正文；留空则自动从 PDF 文本层提取
}

// Result 导入成功后的产出。
type Result struct {
	DocID       string
	VersionID   string
	SHA256      string
	ArticleIDs  []string
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
	// MDPath 留空时自动提取 PDF 文本层（无文本层 = 扫描件，报错转 OCR/人工）
	var mdBytes []byte
	if req.MDPath != "" {
		mdBytes, err = os.ReadFile(req.MDPath)
		if err != nil {
			im.logImport(req, "failed", fmt.Sprintf("读取正文失败: %v", err))
			return nil, fmt.Errorf("读取正文失败: %w", err)
		}
	} else {
		text, xerr := parse.ExtractPDFText(req.PDFPath)
		if xerr != nil {
			im.logImport(req, "failed", xerr.Error())
			return nil, xerr
		}
		mdBytes = []byte(parse.NormalizePDFText(text))
	}
	arts := parse.SplitArticles(string(mdBytes))
	if len(arts) == 0 {
		err := fmt.Errorf("未解析出任何条文（检查「第X条」标记）: %s", req.MDPath)
		im.logImport(req, "failed", err.Error())
		return nil, err
	}

	// 3) 文件落位：pdf/<doc_id>.pdf、md/<doc_id>.md（原始层只读，重复 doc_id 拒绝）
	dstPDF := filepath.Join(im.DataDir, "pdf", req.DocID+".pdf")
	dstMD := filepath.Join(im.DataDir, "md", req.DocID+".md")
	if _, err := os.Stat(dstPDF); err == nil {
		err := fmt.Errorf("doc_id 已存在，禁止覆盖原始层: %s", req.DocID)
		im.logImport(req, "failed", err.Error())
		return nil, err
	}
	if err := copyFile(req.PDFPath, dstPDF); err != nil {
		im.logImport(req, "failed", err.Error())
		return nil, err
	}
	if req.MDPath != "" {
		if err := copyFile(req.MDPath, dstMD); err != nil {
			im.logImport(req, "failed", err.Error())
			return nil, err
		}
	} else if err := os.WriteFile(dstMD, mdBytes, 0o644); err != nil {
		im.logImport(req, "failed", err.Error())
		return nil, err
	}

	// 4) 溯源 JSON（必填字段齐全才允许继续）
	now := time.Now().UTC().Format(time.RFC3339)
	prov := &Provenance{
		DocID: req.DocID, FileName: filepath.Base(dstPDF), FileSize: size,
		SHA256: sha, HashAlgoUsed: "SHA-256", HashComputedAt: now,
		HashComputedBy: "openlaw-ingest/" + versionTag,
		SourceURL:      req.SourceURL, SourceType: req.SourceType, IsGongbao: req.IsGongbao,
		PublishDate: req.PublishDate, EffectiveDate: req.EffectiveDate, ExpireDate: req.ExpireDate,
		EffectiveDateInferred: false,
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
	articleIDs, err := im.writeTx(tx, req, versionID, sha, now, arts)
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
	im.logImport(req, "success", "")

	// 6) 触发断链检测（6.1）。断链不抛异常，随结果返回由上层提示。
	res := &Result{
		DocID: req.DocID, VersionID: versionID, SHA256: sha, ArticleIDs: articleIDs,
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
		return fmt.Errorf("缺少 PDF 路径")
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
func (im *Importer) writeTx(tx *sql.Tx, req Request, versionID, sha, now string, arts []parse.Article) ([]string, error) {
	// 区间重叠检查（13.3）：必须在事务内做，防止同日双版本命中
	if err := checkOverlap(tx, req.LawID, req.EffectiveDate, req.ExpireDate); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`INSERT INTO law(law_id, name, short_name, issuer, effect_level, is_local)
		VALUES(?,?,?,?,?,0)
		ON CONFLICT(law_id) DO UPDATE SET name=excluded.name, short_name=excluded.short_name,
			issuer=excluded.issuer, effect_level=excluded.effect_level`,
		req.LawID, req.LawName, nullable(req.ShortName), nullable(req.Issuer), nullable(req.EffectLevel)); err != nil {
		return nil, fmt.Errorf("写入 law 失败: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO document(doc_id, file_name, file_path, file_size, sha256,
		hash_algo, hash_computed_at, source_url, source_type, is_gongbao, publish_date, confirm_status)
		VALUES(?,?,?,?,?,'SHA-256',?,?,?,?,?,?)`,
		req.DocID, filepath.Base(req.PDFPath), filepath.Join("pdf", req.DocID+".pdf"),
		sizeOrZero(req.PDFPath), sha, now, nullable(req.SourceURL), nullable(req.SourceType),
		boolInt(req.IsGongbao), nullable(req.PublishDate), req.ConfirmStatus); err != nil {
		return nil, fmt.Errorf("写入 document 失败: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO law_version(version_id, law_id, version_no, effective_date,
		expire_date, doc_id, is_gongbao) VALUES(?,?,?,?,?,?,?)`,
		versionID, req.LawID, req.VersionNo, req.EffectiveDate, nullable(req.ExpireDate),
		req.DocID, boolInt(req.IsGongbao)); err != nil {
		return nil, fmt.Errorf("写入 law_version 失败: %w", err)
	}

	mdPath := filepath.Join("md", req.DocID+".md")
	ids := make([]string, 0, len(arts))
	for _, a := range arts {
		articleID := versionID + "_" + a.No
		if _, err := tx.Exec(`INSERT INTO article(article_id, version_id, article_no, content,
			md_path, doc_id, parser_version) VALUES(?,?,?,?,?,?,?)`,
			articleID, versionID, a.No, a.Content, mdPath, req.DocID, parse.ParserVersion); err != nil {
			return nil, fmt.Errorf("写入条文 %s 失败: %w", a.No, err)
		}
		ids = append(ids, articleID)
	}
	return ids, nil
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
