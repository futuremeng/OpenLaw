package sdk

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/futuremeng/OpenLaw/element"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

// OpenLaw 能力层统一入口（3.4）。进程内调用，不启动任何网络服务。
type OpenLaw struct {
	dataDir string
	db      *store.DB
}

// New 打开数据目录（包含 db/、pdf/、md/、provenance/、manifest/）。
func New(dataDir string) (*OpenLaw, error) {
	if _, err := os.Stat(dataDir); err != nil {
		return nil, fmt.Errorf("数据目录不存在: %s", dataDir)
	}
	dbPath := filepath.Join(dataDir, "db", "openlaw.db")
	db, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	return &OpenLaw{dataDir: dataDir, db: db}, nil
}

// Close 关闭数据库。
func (o *OpenLaw) Close() error { return o.db.Close() }

// DataDir 返回数据目录（界面「打开 PDF」按钮需要）。
func (o *OpenLaw) DataDir() string { return o.dataDir }

// ResolveVersion 版本解析（5.3）：按法规名称与时间点解析当时生效版本。
func (o *OpenLaw) ResolveVersion(lawName, atDate string) Result {
	rv, err := validate.ResolveVersion(o.db, lawName, atDate)
	if errors.Is(err, sql.ErrNoRows) {
		if exists, _ := validate.LawExists(o.db, lawName); !exists {
			return fail(StatusNotFound, CodeLawNotFound, ErrorHints[CodeLawNotFound])
		}
		return fail(StatusNotFound, CodeNoVersionAtDate, ErrorHints[CodeNoVersionAtDate])
	} else if err != nil {
		return fail(StatusNotFound, "", fmt.Sprintf("查询失败: %v", err))
	}

	trace := o.traceFor(rv.DocID, rv.PDFPath)
	data := map[string]any{
		"version_id": rv.VersionID, "version_no": rv.VersionNo,
		"effective_date": rv.EffectiveDate, "expire_date": rv.ExpireDate,
		"confirm_status": rv.ConfirmStatus,
	}
	if !trace.ChainOK {
		return brokenChain(data, trace)
	}
	res := ok(data, trace)
	if rv.ConfirmStatus != "confirmed" {
		res.Warnings = append(res.Warnings, "该文本确认状态为待人工确认，仅提示不报错（12.3）")
	}
	return res
}

// CheckCitation 引用校验（5.3、6.2）：法规名 + 条款号 + 引用文本 + 时间点。
func (o *OpenLaw) CheckCitation(lawName, articleNo, quote, atDate string) Result {
	today := time.Now().Format("2006-01-02")
	c, err := validate.CheckCitation(o.db, o.dataDir, lawName, articleNo, quote, atDate, today)
	if err != nil {
		return fail(StatusNotFound, "", fmt.Sprintf("查询失败: %v", err))
	}
	switch c.ErrorCode {
	case validate.CodeLawNotFound:
		return fail(StatusNotFound, CodeLawNotFound, ErrorHints[CodeLawNotFound])
	case validate.CodeNoVersionAtDate:
		return fail(StatusNotFound, CodeNoVersionAtDate, ErrorHints[CodeNoVersionAtDate])
	case validate.CodeArticleNotFound:
		return fail(StatusNotFound, CodeArticleNotFound, ErrorHints[CodeArticleNotFound])
	}

	trace := o.traceFor(c.DocID, c.PDFPath)
	if len(c.ChainIssues) > 0 {
		trace.BreakReason = c.ChainIssues[0].Reason
		trace.ChainOK = false
	}
	data := map[string]any{
		"hit": c.Hit, "similarity": c.Similarity, "expired": c.Expired,
		"article_id": c.ArticleID, "article_no": c.ArticleNo, "content": c.Content,
		"version_no": c.VersionNo, "current_version": c.CurrentVersion,
		"confirm_status": c.ConfirmStatus,
	}

	if !trace.ChainOK {
		// 6.2：溯源失败直接 OL-1006/OL-1007，禁止输出结论
		code := CodeBrokenChain
		if len(c.ChainIssues) > 0 && c.ChainIssues[0].ErrorCode != "" {
			code = c.ChainIssues[0].ErrorCode
		}
		res := brokenChain(data, trace)
		res.ErrorCode = code
		return res
	}

	res := ok(data, trace)
	switch c.ErrorCode {
	case validate.CodeVersionExpired:
		res.Status = StatusExpired
		res.ErrorCode = CodeVersionExpired
		res.Warnings = append(res.Warnings, ErrorHints[CodeVersionExpired])
	case validate.CodeQuoteMismatch:
		res.ErrorCode = CodeQuoteMismatch
		res.Warnings = append(res.Warnings, ErrorHints[CodeQuoteMismatch])
	}
	if c.ConfirmStatus != "confirmed" {
		res.Warnings = append(res.Warnings, "该文本确认状态为待人工确认，仅提示不报错（12.3）")
	}
	return res
}

// AnalyzeElements 要件比对（5.3、6.3）：按案由加载构成要件，比对案件事实。
// 输出风险假设清单（缺失要件与突破方向），不输出胜诉结论。
func (o *OpenLaw) AnalyzeElements(caseType string, facts []string) Result {
	elements, err := element.LoadElements(o.db, caseType)
	if err != nil {
		return fail(StatusNotFound, "", fmt.Sprintf("加载要件失败: %v", err))
	}
	if len(elements) == 0 {
		return fail(StatusNotFound, "", "案由无已录入构成要件，请先人工确认并导入要件")
	}

	matches := element.MatchFacts(elements, facts)
	covered := []map[string]any{}
	missing := []map[string]any{}
	for _, m := range matches {
		if m.Matched {
			covered = append(covered, map[string]any{
				"element_id": m.Element.ElementID, "name": m.Element.Name, "fact": m.Fact,
			})
		} else {
			missing = append(missing, map[string]any{
				"element_id": m.Element.ElementID, "name": m.Element.Name,
				"how_to_attack": m.MissHint,
			})
		}
	}
	res := ok(map[string]any{
		"case_type": caseType, "covered": covered, "missing_elements": missing,
	}, nil)
	res.Warnings = append(res.Warnings, "本结果为风险假设清单，不构成胜诉/败诉结论（2.3）")
	return res
}

// FindRelation 关联查询（5.3）：修改、废止、解释、配套关系。
// 机器不自动判定废止关系，pending 状态必须提示人工确认（10 章）。
func (o *OpenLaw) FindRelation(lawID string) Result {
	rows, err := o.db.Query(`SELECT src_law_id, dst_law_id, relation_type,
		COALESCE(based_on_doc_id, ''), confirm_status
		FROM relation WHERE src_law_id = ? OR dst_law_id = ?`, lawID, lawID)
	if err != nil {
		return fail(StatusNotFound, "", fmt.Sprintf("查询失败: %v", err))
	}
	defer rows.Close()
	var rels []map[string]any
	pending := 0
	for rows.Next() {
		var src, dst, typ, doc, cs string
		if err := rows.Scan(&src, &dst, &typ, &doc, &cs); err != nil {
			return fail(StatusNotFound, "", fmt.Sprintf("读取失败: %v", err))
		}
		if cs != "confirmed" {
			pending++
		}
		rels = append(rels, map[string]any{
			"src_law_id": src, "dst_law_id": dst, "relation_type": typ,
			"based_on_doc_id": doc, "confirm_status": cs,
		})
	}
	if err := rows.Err(); err != nil {
		return fail(StatusNotFound, "", fmt.Sprintf("读取失败: %v", err))
	}
	res := ok(map[string]any{"law_id": lawID, "relations": rels}, nil)
	if pending > 0 {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("%d 条关系待人工确认：修改决定并不必然导致原司法解释废止，机器不自动判定（10 章）", pending))
	}
	return res
}

// DetectBreak 断链检测（5.3、13.4）：返回断链环节与原因。
func (o *OpenLaw) DetectBreak(articleID string) Result {
	issues := validate.CheckArticle(o.db, o.dataDir, articleID)
	if len(issues) == 0 {
		return ok(map[string]any{"article_id": articleID, "chain_ok": true}, nil)
	}
	steps := make([]map[string]string, 0, len(issues))
	for _, is := range issues {
		steps = append(steps, map[string]string{
			"step": is.Step, "reason": is.Reason, "error_code": is.ErrorCode,
		})
	}
	var docID, pdfPath string
	_ = o.db.QueryRow(`SELECT doc_id, (SELECT file_path FROM document WHERE doc_id = a.doc_id)
		FROM article a WHERE article_id = ?`, articleID).Scan(&docID, &pdfPath)
	trace := &Trace{DocID: docID, PDFPath: o.abs(pdfPath), ChainOK: false, BreakReason: issues[0].Reason}
	res := brokenChain(map[string]any{"article_id": articleID, "chain_ok": false, "breaks": steps}, trace)
	if issues[0].ErrorCode != "" {
		res.ErrorCode = issues[0].ErrorCode
	}
	return res
}

// Desensitize 一键脱敏（FR-08）：姓名、案号、身份证号、企业名称。
// 全部本地完成；还原映射仅返回给调用方，不写入日志（7 章）。
func (o *OpenLaw) Desensitize(text string, extraNames ...string) Result {
	redacted, mapping := element.Desensitize(text, extraNames)
	return ok(map[string]any{
		"text": redacted, "restore_map": mapping,
	}, nil)
}

// traceFor 构建溯源信息并顺带执行断链检测（所有输出必须携带 doc_id）。
func (o *OpenLaw) traceFor(docID, relPDF string) *Trace {
	issues := validate.CheckDocument(o.db, o.dataDir, docID)
	t := &Trace{DocID: docID, PDFPath: o.abs(relPDF), ChainOK: len(issues) == 0}
	if !t.ChainOK {
		t.BreakReason = issues[0].Reason
	}
	return t
}

func (o *OpenLaw) abs(rel string) string {
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(o.dataDir, filepath.FromSlash(rel))
}

func brokenChain(data map[string]any, trace *Trace) Result {
	res := ok(data, trace)
	res.Status = StatusBrokenChain
	res.ErrorCode = CodeBrokenChain
	res.Warnings = append(res.Warnings, ErrorHints[CodeBrokenChain])
	return res
}
