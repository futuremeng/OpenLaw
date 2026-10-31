// packs.go 远程数据包（官方 GitHub Releases 通道）：目录刷新与按包安装任务。
// 默认离线纪律：只有用户在界面按下「刷新官方目录」「安装勾选的数据包」才联网；
// 信任链不因传输松动：目录验签 + 受信名单 → 归档 sha256 复核 → 包级 VerifyTrusted，
// 三道闸一道不少。官方密钥未内置（未发版）时如实报「来源不可信」，不降级。
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/pack"
	"github.com/futuremeng/OpenLaw/store"
)

// packsReleaseAPI 官方发布通道（latest release 的 index.json/index.sig 资产）。
// 发版流水线把目录两件套与包归档挂到该 Release；换仓库属装配决策（fork 自部署
// 在自己的构建里改此常量）。
const packsReleaseAPI = "https://api.github.com/repos/futuremeng/OpenLaw/releases/latest"

// PackCard 目录条目的界面视图（法言法语字段：包种类/覆盖范围/条目数/大小）。
type PackCard struct {
	PackID   string   `json:"pack_id"`
	Kind     string   `json:"kind"` // base=基础包 | delta=增量包
	Regions  []string `json:"regions"`
	Since    string   `json:"since"`
	Until    string   `json:"until"`
	DocCount int      `json:"doc_count"`
	Size     int64    `json:"size"`
}

// PackCatalogView 目录刷新结果视图。
type PackCatalogView struct {
	GeneratedAt string     `json:"generated_at"`
	Packs       []PackCard `json:"packs"`
}

// PackCatalogRefresh 拉取并校验官方目录（验签 + 受信名单 + 结构），成功后缓存供
// 安装任务使用。失败如实返回错误——前端必须原样展示，不得渲染空目录冒充可用。
func (a *App) PackCatalogRefresh() (*PackCatalogView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	f := pack.NewFetcher()
	jsonURL, sigURL, err := pack.LatestIndexURLs(ctx, f, packsReleaseAPI)
	if err != nil {
		return nil, fmt.Errorf("官方发布通道查询失败: %w", err)
	}
	c, err := pack.FetchCatalog(ctx, f, jsonURL, sigURL)
	if err != nil {
		return nil, err
	}
	a.packsMu.Lock()
	a.packsCatalog = c
	a.packsMu.Unlock()
	v := &PackCatalogView{GeneratedAt: c.GeneratedAt}
	for _, p := range c.Packs {
		v.Packs = append(v.Packs, PackCard{
			PackID: p.PackID, Kind: p.Kind, Regions: p.Regions,
			Since: p.Since, Until: p.Until, DocCount: p.DocCount, Size: p.Size,
		})
	}
	return v, nil
}

// StartRemoteInstallTask 按勾选安装：单槽任务（互斥、可取消），串行安装
// （基础包在前、增量包按日期），每包独立下载校验导入；中断/失败如实保留已装部分
// （台账可查，重跑幂等续装）。
func (a *App) StartRemoteInstallTask(packIDs []string) (TaskInfo, error) {
	if len(packIDs) == 0 {
		return TaskInfo{}, errors.New("请先勾选要安装的数据包")
	}
	a.packsMu.Lock()
	c := a.packsCatalog
	a.packsMu.Unlock()
	if c == nil {
		return TaskInfo{}, errors.New("请先刷新官方目录，再选择要安装的数据包")
	}
	return a.tm.Submit("pack-install", "packs="+strings.Join(packIDs, ","),
		func(ctx context.Context, progress func(string)) error {
			db, err := store.Open(filepath.Join(a.dataDir, "db", "openlaw.db"))
			if err != nil {
				return err
			}
			defer db.Close()
			f := pack.NewFetcher()
			rep, err := pack.InstallPacks(ctx, db, a.dataDir, f, c, packIDs, progress)
			imported := 0
			for _, ir := range rep.Reports {
				imported += ir.Imported
			}
			if err != nil {
				return err
			}
			progress(fmt.Sprintf("安装完成：%d 个包，新增 %d 条（进入待复核）", len(rep.Reports), imported))
			return nil
		})
}
