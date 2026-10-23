package validate

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/futuremeng/OpenLaw/hashutil"
	"github.com/futuremeng/OpenLaw/store"
)

// 断链环节（开发文档 13.4）：法规→版本、版本→文档、文档→文件、文件→哈希、哈希→manifest、manifest→签名。
const (
	StepVersion   = "version"
	StepDocument  = "document"
	StepFile      = "file"
	StepHash      = "hash"
	StepManifest  = "manifest"
	StepSignature = "signature"
)

// 错误码（开发文档 5.4）。断链不抛异常，统一走结构化结果。
const (
	CodeBrokenChain   = "OL-1006"
	CodeHashFail      = "OL-1007"
	CodeSignatureFail = "OL-1008"
)

// Issue 一个断链环节及其原因。
type Issue struct {
	Step      string `json:"step"`
	Reason    string `json:"reason"`
	ErrorCode string `json:"error_code"`
}

// CheckDocument 从文档出发校验溯源链：文件存在 → 哈希一致 → manifest 收录 → 签名有效。
// 每个失败环节写入 validate_log。
func CheckDocument(db *store.DB, dataDir, docID string) []Issue {
	var issues []Issue

	// 环节：文档记录存在
	var filePath, shaStored, algo string
	err := db.QueryRow(`SELECT file_path, sha256, hash_algo FROM document WHERE doc_id = ?`, docID).
		Scan(&filePath, &shaStored, &algo)
	if errors.Is(err, sql.ErrNoRows) {
		return logIssues(db, "document", docID, []Issue{{
			Step: StepDocument, Reason: "document 记录缺失", ErrorCode: CodeBrokenChain}})
	} else if err != nil {
		return logIssues(db, "document", docID, []Issue{{
			Step: StepDocument, Reason: fmt.Sprintf("查询 document 失败: %v", err), ErrorCode: CodeBrokenChain}})
	}

	// 环节：文件存在（file_path 相对数据目录）
	absPath := abs(dataDir, filePath)
	if _, err := os.Stat(absPath); err != nil {
		return logIssues(db, "document", docID, []Issue{{
			Step: StepFile, Reason: fmt.Sprintf("PDF 文件缺失: %s", filePath), ErrorCode: CodeBrokenChain}})
	}

	// 环节：哈希一致（重算比对，OL-1007 立即告警并隔离文件由上层处理）
	shaNow, err := hashutil.SHA256File(absPath)
	if err != nil || shaNow != shaStored {
		return logIssues(db, "document", docID, []Issue{{
			Step: StepHash, Reason: fmt.Sprintf("SHA-256 不一致（存储 %s…，重算 %s…）", trunc(shaStored), trunc(shaNow)),
			ErrorCode: CodeHashFail}})
	}

	// 环节：manifest 收录该文件且条目哈希一致
	entry, err := ManifestEntryFor(dataDir, filePath)
	if errors.Is(err, ErrManifestMissing) {
		return logIssues(db, "document", docID, []Issue{{
			Step: StepManifest, Reason: "manifest 缺失，未纳入每日防篡改校验", ErrorCode: CodeBrokenChain}})
	} else if err != nil {
		return logIssues(db, "document", docID, []Issue{{
			Step: StepManifest, Reason: err.Error(), ErrorCode: CodeHashFail}})
	}
	if entry.SHA256 != shaStored {
		return logIssues(db, "document", docID, []Issue{{
			Step: StepManifest, Reason: fmt.Sprintf("manifest 条目哈希与 document 记录不一致: %s", filePath),
			ErrorCode: CodeHashFail}})
	}

	// 环节：manifest 签名有效（OL-1008 停止使用数据目录由上层处理）
	if err := VerifyManifest(dataDir); err != nil {
		code := CodeSignatureFail
		if errors.Is(err, ErrManifestMissing) {
			code = CodeBrokenChain
		}
		if errors.Is(err, ErrFileMismatch) && !errors.Is(err, ErrSignatureInvalid) {
			code = CodeHashFail
		}
		return logIssues(db, "document", docID, []Issue{{
			Step: StepSignature, Reason: err.Error(), ErrorCode: code}})
	}

	logPass(db, "document", docID)
	return issues
}

// CheckArticle 从条文出发校验完整溯源链：条文→版本→文档→后续环节。
func CheckArticle(db *store.DB, dataDir, articleID string) []Issue {
	var versionID, docID string
	err := db.QueryRow(`SELECT version_id, doc_id FROM article WHERE article_id = ?`, articleID).
		Scan(&versionID, &docID)
	if errors.Is(err, sql.ErrNoRows) {
		return logIssues(db, "article", articleID, []Issue{{
			Step: StepDocument, Reason: "article 记录缺失", ErrorCode: CodeBrokenChain}})
	} else if err != nil {
		return logIssues(db, "article", articleID, []Issue{{
			Step: StepDocument, Reason: fmt.Sprintf("查询 article 失败: %v", err), ErrorCode: CodeBrokenChain}})
	}

	// 环节：版本存在
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM law_version WHERE version_id = ?`, versionID).Scan(&n); err != nil || n == 0 {
		return logIssues(db, "article", articleID, []Issue{{
			Step: StepVersion, Reason: fmt.Sprintf("版本记录缺失: %s", versionID), ErrorCode: CodeBrokenChain}})
	}

	issues := CheckDocument(db, dataDir, docID)
	if len(issues) > 0 {
		logIssues(db, "article", articleID, issues)
	}
	return issues
}

// CheckAllArticles 全量断链扫描，返回 articleID → 断链环节（9.2 每周断链清单）。
func CheckAllArticles(db *store.DB, dataDir string) (map[string][]Issue, error) {
	rows, err := db.Query(`SELECT article_id FROM article ORDER BY article_id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := map[string][]Issue{}
	for _, id := range ids {
		if issues := CheckArticle(db, dataDir, id); len(issues) > 0 {
			out[id] = issues
		}
	}
	return out, nil
}

func abs(dataDir, rel string) string {
	if len(rel) > 0 && (rel[0] == '/' || (len(rel) > 1 && rel[1] == ':')) {
		return rel
	}
	return dataDir + "/" + rel
}

func trunc(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func logIssues(db *store.DB, targetType, targetID string, issues []Issue) []Issue {
	for _, is := range issues {
		_, _ = db.Exec(`INSERT INTO validate_log(target_type, target_id, validate_type, result, reason, created_at)
			VALUES(?,?,?, 'fail', ?, ?)`, targetType, targetID, "chain:"+is.Step, is.Reason,
			time.Now().UTC().Format(time.RFC3339))
	}
	return issues
}

func logPass(db *store.DB, targetType, targetID string) {
	_, _ = db.Exec(`INSERT INTO validate_log(target_type, target_id, validate_type, result, reason, created_at)
		VALUES(?,?, 'chain', 'pass', '', ?)`, targetType, targetID, time.Now().UTC().Format(time.RFC3339))
}
