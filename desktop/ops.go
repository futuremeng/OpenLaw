// ops.go 运维面板：全库体检 / manifest / 离线更新包。全部走与
// openlaw-check / openlaw-pack 完全相同的库层代码路径（validate.* / pack.*），
// 桌面端不发明第二套运维逻辑。签名私钥只经路径引用，内容绝不进 UI/日志。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/futuremeng/OpenLaw/pack"
	"github.com/futuremeng/OpenLaw/store"
	"github.com/futuremeng/OpenLaw/validate"
)

// BrokenRow 断链明细一行（validate.Issue 展平）。
type BrokenRow struct {
	Key       string `json:"key"`
	Step      string `json:"step"`
	Reason    string `json:"reason"`
	ErrorCode string `json:"error_code"`
}

// CheckView 全库体检结果（openlaw-check 同路径：VerifyManifest + CheckAllArticles）。
type CheckView struct {
	ManifestOK  bool        `json:"manifest_ok"`
	ManifestErr string      `json:"manifest_err,omitempty"`
	BrokenCount int         `json:"broken_count"`
	Broken      []BrokenRow `json:"broken,omitempty"`
	Truncated   bool        `json:"truncated"` // 明细只回传前 100 条，超出如实标注
}

// CorpusCheck 全库体检（只读）。
func (a *App) CorpusCheck() (CheckView, error) {
	if a.db == nil {
		return CheckView{}, errors.New(a.openErr)
	}
	var v CheckView
	if err := validate.VerifyManifest(a.dataDir); err != nil {
		v.ManifestOK, v.ManifestErr = false, err.Error()
	} else {
		v.ManifestOK = true
	}
	broken, err := validate.CheckAllArticles(a.db, a.dataDir)
	if err != nil {
		return v, err
	}
	v.BrokenCount = len(broken)
	keys := make([]string, 0, len(broken))
	for k := range broken {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, is := range broken[k] {
			if len(v.Broken) >= 100 {
				v.Truncated = true
				break
			}
			v.Broken = append(v.Broken, BrokenRow{Key: k, Step: is.Step, Reason: is.Reason, ErrorCode: is.ErrorCode})
		}
		if v.Truncated {
			break
		}
	}
	return v, nil
}

// ManifestView manifest 六要素 + 签名状态（读 manifest.json，不重算）。
type ManifestView struct {
	Exists          bool   `json:"exists"`
	GeneratedAt     string `json:"generated_at,omitempty"`
	ToolVersion     string `json:"tool_version,omitempty"`
	ProducerVersion string `json:"producer_version,omitempty"`
	SchemaVersion   string `json:"schema_version,omitempty"`
	License         string `json:"license,omitempty"`
	Attribution     string `json:"attribution,omitempty"`
	Files           int    `json:"files"`
	VerifyErr       string `json:"verify_err,omitempty"`
}

// ManifestStatus 读取并校验 manifest（只读）。
func (a *App) ManifestStatus() ManifestView {
	m, _, err := validate.LoadManifest(a.dataDir)
	if err != nil {
		return ManifestView{Exists: false, VerifyErr: err.Error()}
	}
	v := ManifestView{Exists: true, GeneratedAt: m.GeneratedAt, ToolVersion: m.ToolVersion,
		ProducerVersion: m.ProducerVersion, SchemaVersion: m.SchemaVersion,
		License: m.License, Attribution: m.Attribution, Files: len(m.Files)}
	if err := validate.VerifyManifest(a.dataDir); err != nil {
		v.VerifyErr = err.Error()
	}
	return v
}

// resolvePackDir 相对路径按数据目录解析。
func (a *App) resolvePackDir(p string) string {
	if !filepath.IsAbs(p) {
		return filepath.Join(a.dataDir, p)
	}
	return p
}

// PackVerify 更新包校验（只读；返回空串 = 通过）。
func (a *App) PackVerify(packDir string) string {
	if packDir == "" {
		return "必须指定包目录"
	}
	if err := pack.Verify(a.resolvePackDir(packDir)); err != nil {
		return err.Error()
	}
	return ""
}

// StartPackBuildTask 生成并签名离线更新包（pack.Build——与 openlaw-pack 同路径）。
func (a *App) StartPackBuildTask(packDir, keyPath, license, attribution string) (TaskInfo, error) {
	if packDir == "" || keyPath == "" {
		return TaskInfo{}, errors.New("生成数据包需要指定数据包目录与签名私钥路径（私钥不得放入数据目录）")
	}
	if _, err := os.Stat(keyPath); err != nil {
		return TaskInfo{}, fmt.Errorf("私钥不可读: %w", err)
	}
	pd := a.resolvePackDir(packDir)
	params := fmt.Sprintf("pack=%s key=%s", filepath.Base(pd), keyPath)
	return a.tm.Submit("pack-build", params, func(ctx context.Context, progress func(string)) error {
		db, err := store.Open(filepath.Join(a.dataDir, "db", "openlaw.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		progress("构建中（哈希重算 + 文件复制 + 签名）…")
		if err := pack.Build(db, a.dataDir, pd, keyPath, license, attribution); err != nil {
			return err
		}
		progress("更新包已生成并签名: " + pd)
		return nil
	})
}

// StartPackImportTask 校验通过后导入更新包（pack.Import——失败保留旧数据）。
func (a *App) StartPackImportTask(packDir string) (TaskInfo, error) {
	if packDir == "" {
		return TaskInfo{}, errors.New("请先指定数据包目录")
	}
	pd := a.resolvePackDir(packDir)
	return a.tm.Submit("pack-import", "pack="+filepath.Base(pd), func(ctx context.Context, progress func(string)) error {
		db, err := store.Open(filepath.Join(a.dataDir, "db", "openlaw.db"))
		if err != nil {
			return err
		}
		defer db.Close()
		progress("校验并导入中…")
		results, err := pack.Import(db, a.dataDir, pd)
		progress(fmt.Sprintf("已导入 %d 条（均 pending 转人工）", len(results)))
		return err
	})
}

// ReloadRiskCache 强制丢弃风险标记缓存（下次读队列重扫 provenance）。
func (a *App) ReloadRiskCache() {
	a.riskMu.Lock()
	a.risk = nil
	a.riskMu.Unlock()
}
