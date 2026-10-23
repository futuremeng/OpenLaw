package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Provenance 溯源文件（开发文档 4.3）。每个 PDF 对应一个同名 JSON。
// 必填字段缺失即校验失败，禁止入库。
type Provenance struct {
	// ---- 必填 ----
	DocID                 string `json:"doc_id"`
	FileName              string `json:"file_name"`
	FileSize              int64  `json:"file_size"`
	SHA256                string `json:"sha256"`
	HashAlgoUsed          string `json:"hash_algo_used"` // 必须注明算法类型：SHA-256 / SHA-512 / SM3
	HashComputedAt        string `json:"hash_computed_at"`
	HashComputedBy        string `json:"hash_computed_by"`
	SourceURL             string `json:"source_url"` // 可为空串（本地导入），但字段必须存在
	SourceType            string `json:"source_type"`
	IsGongbao             bool   `json:"is_gongbao"`
	PublishDate           string `json:"publish_date"`
	EffectiveDate         string `json:"effective_date"`
	ExpireDate            string `json:"expire_date"` // 空串 = 未失效（左闭右开区间约定）
	EffectiveDateInferred bool   `json:"effective_date_inferred"`
	MDPath                string `json:"md_path"`
	ConfirmStatus         string `json:"confirm_status"` // confirmed / pending / rejected
	// ---- 可选 ----
	MD5             string   `json:"md5,omitempty"`
	SM3             string   `json:"sm3,omitempty"`
	OCRUsed         bool     `json:"ocr_used,omitempty"`
	OCRConfidence   *float64 `json:"ocr_confidence,omitempty"`
	PDFHasSignature *bool    `json:"pdf_has_signature,omitempty"`
	SignatureValid  *bool    `json:"signature_valid,omitempty"`
	TimestampToken  string   `json:"timestamp_token,omitempty"`
	Notes           string   `json:"notes,omitempty"`
}

// Validate 必填字段完整性校验（4.3：source_url 允许空串但键必须存在，
// encoding/json 对 string 缺键会留零值，因此对 URL 类字段只要求键存在过即可）。
func (p *Provenance) Validate() error {
	missing := []string{}
	if p.DocID == "" {
		missing = append(missing, "doc_id")
	}
	if p.FileName == "" {
		missing = append(missing, "file_name")
	}
	if p.SHA256 == "" {
		missing = append(missing, "sha256")
	}
	if p.HashAlgoUsed == "" {
		missing = append(missing, "hash_algo_used")
	}
	if p.HashComputedAt == "" {
		missing = append(missing, "hash_computed_at")
	}
	if p.HashComputedBy == "" {
		missing = append(missing, "hash_computed_by")
	}
	if p.SourceType == "" {
		missing = append(missing, "source_type")
	}
	if p.EffectiveDate == "" {
		missing = append(missing, "effective_date")
	}
	if p.MDPath == "" {
		missing = append(missing, "md_path")
	}
	if p.ConfirmStatus == "" {
		missing = append(missing, "confirm_status")
	}
	if len(missing) > 0 {
		return fmt.Errorf("溯源文件缺少必填字段: %v", missing)
	}
	return nil
}

// LoadProvenance 读取并校验溯源 JSON。
func LoadProvenance(path string) (*Provenance, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取溯源文件失败: %w", err)
	}
	var p Provenance
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("解析溯源文件失败: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Save 写入数据目录 provenance/<doc_id>.json。
func (p *Provenance) Save(dataDir string) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	dir := filepath.Join(dataDir, "provenance")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, p.DocID+".json")
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
