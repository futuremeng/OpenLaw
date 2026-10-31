// fetch.go 远程获取通道（GitHub Releases 等）：目录拉取、归档下载与多包按序安装。
// 纪律与全部采集通道一致：默认离线——本文件不被调用则绝不联网；只由用户显式触发；
// 礼貌限速；中断/失败如实保留部分成果（已装包在 pack_log 台账，未装包不碰）。
// 信任链不因传输而松动：目录验签 + 归档 sha256 复核 + 包级 VerifyTrusted 三道闸原样执行。
package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// FetchUA 如实标识（与抓取通道同一纪律）。
const FetchUA = ToolVersion + " (OpenLaw offline update packs)"

// Fetcher HTTP 客户端 + 礼貌限速（两次请求之间的最小间隔）。
type Fetcher struct {
	Client      *http.Client
	MinInterval time.Duration
	last        time.Time
}

// NewFetcher 默认限速 1s/请求（下载是大文件，间隔主要保护元数据端点）。
func NewFetcher() *Fetcher {
	return &Fetcher{Client: &http.Client{Timeout: 30 * time.Minute}, MinInterval: time.Second}
}

func (f *Fetcher) polite(ctx context.Context) error {
	if d := time.Since(f.last); d < f.MinInterval {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.MinInterval - d):
		}
	}
	f.last = time.Now()
	return nil
}

func (f *Fetcher) get(ctx context.Context, url string) ([]byte, error) {
	if err := f.polite(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", FetchUA)
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 %s 失败: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("下载 %s 失败: 响应为空", url)
	}
	return body, nil
}

// LatestIndexURLs 从 GitHub Releases API（latest）取目录两件套的下载直链。
// apiURL 由调用方给出（桌面内置官方仓标识；测试注入 httptest）。
func LatestIndexURLs(ctx context.Context, f *Fetcher, apiURL string) (jsonURL, sigURL string, err error) {
	body, err := f.get(ctx, apiURL)
	if err != nil {
		return "", "", err
	}
	var rel struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", fmt.Errorf("发布通道响应不可解析: %w", err)
	}
	for _, a := range rel.Assets {
		switch a.Name {
		case "index.json":
			jsonURL = a.BrowserDownloadURL
		case "index.sig":
			sigURL = a.BrowserDownloadURL
		}
	}
	if jsonURL == "" || sigURL == "" {
		return "", "", fmt.Errorf("发布通道缺少 index.json / index.sig 资产")
	}
	return jsonURL, sigURL, nil
}

// FetchCatalog 下载目录两件套并走 ParseCatalog（验签 + 受信 + 结构）。
// 目录不完整或被裁剪一律拒收，调用方不得降级使用。
func FetchCatalog(ctx context.Context, f *Fetcher, jsonURL, sigURL string) (*Catalog, error) {
	jsonBody, err := f.get(ctx, jsonURL)
	if err != nil {
		return nil, err
	}
	sigBody, err := f.get(ctx, sigURL)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "openlaw-index-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), jsonBody, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.sig"), sigBody, 0o644); err != nil {
		return nil, err
	}
	return ParseCatalog(dir)
}

// DownloadPack 下载并暂存一个包：归档先对 sha256（目录锚定），再解包，再 VerifyTrusted。
// 暂存区 <stagingBase>/<pack_id>/ 已有校验通过的包则复用（断点续装）；
// 半成品不清除不覆盖——如实报错由人工处置。
func DownloadPack(ctx context.Context, f *Fetcher, cp CatalogPack, stagingBase string) (string, error) {
	packDir := filepath.Join(stagingBase, cp.PackID)
	if err := VerifyTrusted(packDir); err == nil {
		return packDir, nil // 已暂存且校验通过：续装直接复用，不重复下载
	}
	if _, err := os.Stat(packDir); err == nil {
		return "", fmt.Errorf("暂存区 %s 存在未通过校验的半成品，拒绝覆盖（请删除该目录后重试）", cp.PackID)
	}
	if err := os.MkdirAll(stagingBase, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(stagingBase, cp.PackID+".zip")
	tmpPath := zipPath + ".part"
	if err := f.polite(ctx); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cp.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", FetchUA)
	resp, err := f.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 %s 失败: HTTP %d", cp.PackID, resp.StatusCode)
	}
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), resp.Body)
	cerr := out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if cerr != nil {
		os.Remove(tmpPath)
		return "", cerr
	}
	sha := hex.EncodeToString(h.Sum(nil))
	if sha != cp.SHA256 {
		os.Remove(tmpPath)
		return "", fmt.Errorf("%w: %s 下载内容哈希与目录不符（传输损坏或源被篡改）", ErrFileMismatch, cp.PackID)
	}
	if err := os.Rename(tmpPath, zipPath); err != nil {
		return "", err
	}
	if err := Unarchive(zipPath, packDir); err != nil {
		return "", err
	}
	if err := VerifyTrusted(packDir); err != nil {
		return "", fmt.Errorf("暂存包校验失败: %w", err)
	}
	return packDir, nil
}

// InstallReport 多包安装结果（逐包如实：装了/跳过/已装过，中断处标明）。
type InstallReport struct {
	Reports     []ImportReport
	Interrupted string // 非空 = 中止于该包（其余包不受影响，台账可续装）
}

// InstallPacks 按「基础包在前、增量包按标识（含日期）升序」串行安装所选包。
// 每包独立走 DownloadPack → ImportPack；任一包失败即停（已装包保留、台账可查），
// 如实返回已完成部分与中止包标识——绝不假装完整成功。
func InstallPacks(ctx context.Context, db *store.DB, dataDir string, f *Fetcher, c *Catalog, packIDs []string, progress func(string)) (InstallReport, error) {
	want := map[string]bool{}
	for _, id := range packIDs {
		want[id] = true
	}
	var chosen []CatalogPack
	for _, p := range c.Packs {
		if want[p.PackID] {
			chosen = append(chosen, p)
		}
	}
	for _, id := range packIDs {
		found := false
		for _, p := range chosen {
			if p.PackID == id {
				found = true
				break
			}
		}
		if !found {
			return InstallReport{}, fmt.Errorf("%w: 目录中没有包 %q", ErrBadCatalog, id)
		}
	}
	sort.SliceStable(chosen, func(i, j int) bool {
		ki, kj := chosen[i].Kind, chosen[j].Kind
		if (ki == "base") != (kj == "base") {
			return ki == "base"
		}
		if chosen[i].Since != chosen[j].Since {
			return chosen[i].Since < chosen[j].Since
		}
		return chosen[i].PackID < chosen[j].PackID
	})
	stagingBase := filepath.Join(dataDir, "packs")
	rep := InstallReport{}
	for _, cp := range chosen {
		if err := ctx.Err(); err != nil {
			rep.Interrupted = cp.PackID
			return rep, fmt.Errorf("安装已中止（%s 未开始，已装条目保留）: %w", cp.PackID, err)
		}
		if progress != nil {
			progress(fmt.Sprintf("正在获取 %s…", cp.PackID))
		}
		packDir, err := DownloadPack(ctx, f, cp, stagingBase)
		if err != nil {
			rep.Interrupted = cp.PackID
			return rep, fmt.Errorf("获取 %s 失败（已装条目保留）: %w", cp.PackID, err)
		}
		ir, err := ImportPack(db, dataDir, packDir)
		rep.Reports = append(rep.Reports, ir)
		if err != nil {
			rep.Interrupted = cp.PackID
			return rep, fmt.Errorf("导入 %s 失败（已装条目保留，台账记 partial 可续装）: %w", cp.PackID, err)
		}
		if progress != nil {
			switch {
			case ir.AlreadyApplied:
				progress(fmt.Sprintf("%s：此前已安装，跳过", cp.PackID))
			default:
				progress(fmt.Sprintf("%s：导入 %d 条，跳过重复 %d 条", cp.PackID, ir.Imported, ir.Skipped))
			}
		}
	}
	return rep, nil
}
