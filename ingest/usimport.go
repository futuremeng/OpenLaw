// usimport.go 美国全文解析入库（USLM XML → law/law_version/article，
// docs/design-multi-jurisdiction.md §6）。CLI（openlaw-us -ingest）与桌面端
// 共用本 runner——同一代码路径纪律。
//
// 写路径纪律：新条目一律 confirm_status=pending（人工闸口）；生效日期必须由
// 用户随批量包元数据提供（-effective），绝不从包体猜日期；原始 XML 文件按
// 原样归档 xml/ 层并记 SHA-256；解析器版本随 article.parser_version 落库。
package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// USLMImportOptions USLM 全文导入参数（与 openlaw-us -ingest flag 同语义）。
type USLMImportOptions struct {
	XMLPath     string // USLM XML 文件（批量包解出文件；绝对路径或相对数据目录）
	DocID       string // 条目编号（如 us_ecfr_title12_2025）
	LawID       string // 留空 = 与 DocID 同
	Name        string // 法规名称；留空用解析出的标题，仍为空则报错（不得造名）
	Issuer      string // 发布机关（如 Office of the Comptroller of the Currency）
	EffectLevel string // 效力等级（默认 联邦规章；决定法域推导 US）
	VersionNo   string // 版本号（如 2025 annual edition；留空 = 生效日期版）
	EditionDate string // 版本生效日期 ISO（必填——批量包元数据提供，不得猜）
	PublishDate string // 公布日期 ISO（可空 = 未知）
	SourceURL   string // 批量包来源地址（清单留证核对用）
	DryRun      bool   // 只解析不入库
}

// USLMImportResult 导入结果（dry-run 时 OnlyParsed=true，其余字段如实）。
type USLMImportResult struct {
	DocID         string `json:"doc_id"`
	VersionID     string `json:"version_id"`
	LawID         string `json:"law_id"`
	Title         string `json:"title"`
	Citation      string `json:"citation"`
	Articles      int    `json:"articles"`
	SHA256        string `json:"sha256"`
	ParserVersion string `json:"parser_version"`
	ConfirmStatus string `json:"confirm_status"`
	ChainIssues   int    `json:"chain_issues"` // 导入即检的断链问题数（0=链路完好）
	DryRun        bool   `json:"dry_run,omitempty"`
}

// RunUSLMImport 解析并导入一个 USLM 文件。任何一步失败都如实报错，不留半成品。
func RunUSLMImport(dataDir string, db *store.DB, o USLMImportOptions) (*USLMImportResult, error) {
	if o.EffectLevel == "" {
		o.EffectLevel = "联邦规章"
	}
	if DeriveJurisdiction(o.EffectLevel) != "US" {
		return nil, fmt.Errorf("效力等级 %q 不属美国法域——USLM 通道效力等级只能是美国各级（docs/sources-us.md）", o.EffectLevel)
	}
	if o.EditionDate == "" {
		return nil, fmt.Errorf("生效日期必填：来自批量包元数据（年度版日期），不得从包体猜测")
	}
	xmlPath := o.XMLPath
	if !filepath.IsAbs(xmlPath) {
		xmlPath = filepath.Join(dataDir, xmlPath)
	}
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		return nil, fmt.Errorf("读取 USLM 文件失败: %w", err)
	}
	parsed, err := parse.ParseUSLM(data)
	if err != nil {
		return nil, err
	}
	name := o.Name
	if name == "" {
		name = parsed.Title
	}
	if name == "" {
		return nil, fmt.Errorf("法规名称缺失：文件无标题且未提供 -name（不得造名）")
	}
	if o.LawID == "" {
		o.LawID = o.DocID
	}
	if o.VersionNo == "" {
		o.VersionNo = o.EditionDate + "版"
	}
	var md strings.Builder
	fmt.Fprintf(&md, "# %s\n\n", name)
	if parsed.Citation != "" {
		fmt.Fprintf(&md, "来源引注：%s\n\n", parsed.Citation)
	}
	for _, a := range parsed.Articles {
		fmt.Fprintf(&md, "%s\n%s\n\n", a.No, a.Content)
	}
	res := &USLMImportResult{LawID: o.LawID, Title: name, Citation: parsed.Citation,
		Articles: len(parsed.Articles), ParserVersion: parse.ParserVersionUS,
		ConfirmStatus: "pending", DryRun: o.DryRun}
	if o.DryRun {
		return res, nil
	}
	im := &Importer{DataDir: dataDir, DB: db}
	ir, err := im.Import(Request{
		DocID: o.DocID, LawID: o.LawID, LawName: name, Issuer: o.Issuer,
		EffectLevel: o.EffectLevel, VersionNo: o.VersionNo,
		EffectiveDate: o.EditionDate, PublishDate: o.PublishDate,
		SourceURL: o.SourceURL, SourceType: "us-govinfo",
		ConfirmStatus: "pending", Lang: "eng",
		Notes:         fmt.Sprintf("govinfo USLM 全文通道：%s；解析器 %s", parsed.Citation, parse.ParserVersionUS),
		PDFPath:       xmlPath,
		Articles:      parsed.Articles,
		MDContent:     md.String(),
		ParserVersion: parse.ParserVersionUS,
	})
	if err != nil {
		return nil, err
	}
	res.DocID, res.VersionID, res.SHA256 = ir.DocID, ir.VersionID, ir.SHA256
	res.ChainIssues = len(ir.ChainIssues)
	return res, nil
}
