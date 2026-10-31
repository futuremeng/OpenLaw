// archive.go 更新包归档（远程分发形态）：包目录 ↔ 单个 zip。
// 远程渠道（GitHub Releases 等）按单文件资产分发；下载后解进暂存区，再走与本地包
// 完全相同的 VerifyTrusted → ImportPack 链路——归档只是运输壳，不改变任何校验纪律。
package pack

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Archive 把包目录打成 zip（zip 根 = 包目录内容：manifest.json 在根，不多套一层）。
// 拒绝打包缺 manifest.json 的目录——半截目录不得流入分发渠道。
func Archive(packDir, zipPath string) error {
	if _, err := os.Stat(filepath.Join(packDir, "manifest.json")); err != nil {
		return fmt.Errorf("%w: %s", ErrManifestMissing, packDir)
	}
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	err = filepath.Walk(packDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(packDir, p)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}

// Unarchive 把 zip 解到目标目录（暂存区），拒绝越界路径（zip-slip）与覆盖已有文件。
// 解包不校验签名——校验由随后的 Verify/VerifyTrusted 负责，保持同一道闸。
func Unarchive(zipPath, targetDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("更新包归档不可读: %w", err)
	}
	defer zr.Close()
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	for _, zf := range zr.File {
		name := filepath.Clean(filepath.FromSlash(zf.Name))
		if name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) || filepath.IsAbs(name) {
			return fmt.Errorf("%w: 归档含越界路径 %q", ErrExtraFile, zf.Name)
		}
		target := filepath.Join(targetDir, name)
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("暂存区已存在 %s，拒绝覆盖（如实清理后重试）", name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}
