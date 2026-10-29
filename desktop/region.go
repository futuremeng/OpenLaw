// region.go 地点定位与按地域显示（OL-12 读侧）。
//
// 纪律：
//   - 默认离线——定位由用户显式点击触发，浏览器只交出经纬度，判定在 Go 侧用
//     内置 geo 数据离线完成；不联网、不做 IP 定位、不调用任何地理编码服务。
//   - 就近判定必然带误差：距离如实回传并在 UI 展示，用户可一键更正为手动选择。
//   - 法规的地域来自名称推导（parse.InferRegion，保守规则）；推不出的一律显示
//     「地域未标注」，绝不假装覆盖。写库标引（law.region_code + 人工复核）另期。
//   - 地区偏好存 meta 表，经 Go 读写（前端不私存业务状态），随数据目录迁移。
package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/geo"
	"github.com/futuremeng/OpenLaw/parse"
)

// meta 表中的地区偏好键（加法式使用，不动 schema）。
const (
	metaRegion       = "user_region"
	metaRegionSource = "user_region_source" // auto = 就近判定 / manual = 用户选择
	metaRegionAt     = "user_region_at"
	metaRegionNote   = "user_region_note" // 误差等如实说明
)

// RegionOption 地区下拉一项。
type RegionOption struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Parent string `json:"parent"` // 所属省 Code（省级为空）
	Level  int    `json:"level"`  // 1=省 2=地级
}

// RegionState 当前地区（供 UI 贯穿显示）。
type RegionState struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Path       string  `json:"path"` // 「河北省 · 石家庄市」
	Source     string  `json:"source"`
	At         string  `json:"at"`
	Note       string  `json:"note"` // 就近判定误差 / 未设置原因（如实）
	DistanceKm float64 `json:"distance_km"`
}

// LocateResult 定位判定结果（不抛错，原因在 Msg 里如实给出）。
type LocateResult struct {
	OK     bool        `json:"ok"`
	Msg    string      `json:"msg"`
	Region RegionState `json:"region"`
}

// metaGet / metaSet 读写 meta 表键值（库未打开时如实返回空/错误）。
func (a *App) metaGet(key string) string {
	if a.db == nil {
		return ""
	}
	var v string
	_ = a.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	return v
}

func (a *App) metaSet(key, value string) error {
	if a.db == nil {
		return errors.New("数据库未打开，无法保存地区偏好")
	}
	_, err := a.db.Exec(`INSERT INTO meta(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// RegionOptions 省 → 地级两级列表（按区划码顺序），供前端下拉。
func (a *App) RegionOptions() []RegionOption {
	var out []RegionOption
	for _, p := range geo.Provinces() {
		out = append(out, RegionOption{Code: p.Code, Name: p.Name, Level: p.Level})
		for _, c := range geo.Children(p.Code) {
			if c.Name == p.Name { // 直辖市：省级与地级同名，下拉里不重复列
				continue
			}
			out = append(out, RegionOption{Code: c.Code, Name: c.Name, Parent: p.Code, Level: c.Level})
		}
	}
	return out
}

// CurrentRegion 当前地区偏好；未设置时 Code 为空并在 Note 里说明原因。
func (a *App) CurrentRegion() RegionState {
	code := a.metaGet(metaRegion)
	st := RegionState{
		Code:       code,
		Source:     a.metaGet(metaRegionSource),
		At:         a.metaGet(metaRegionAt),
		Note:       a.metaGet(metaRegionNote),
		DistanceKm: -1,
	}
	if code == "" {
		if st.Note == "" {
			st.Note = "尚未设置地区：可点「获取我的位置」（仅本机离线判定）或手动选择"
		}
		return st
	}
	st.Name = geo.NameOf(code)
	st.Path = geo.PathName(code)
	if st.Name == "" {
		st.Note = "已保存的地区代码 " + code + " 不在内置区划表内（区划调整？请重新选择）"
	}
	return st
}

// SetRegion 手动选择地区（code 空 = 不限地区）。返回空串 = 成功，非空 = 错误文案。
func (a *App) SetRegion(code string) string {
	c := strings.TrimSpace(code)
	if c != "" {
		if _, ok := geo.ByCode(c); !ok {
			return "地区代码无效：" + c + "（不在内置区划表内，请重新选择）"
		}
	}
	if c == "" {
		for _, k := range []string{metaRegion, metaRegionSource, metaRegionAt, metaRegionNote} {
			if err := a.metaSet(k, ""); err != nil {
				return err.Error()
			}
		}
		return ""
	}
	if err := a.metaSet(metaRegion, c); err != nil {
		return err.Error()
	}
	_ = a.metaSet(metaRegionSource, "manual")
	_ = a.metaSet(metaRegionAt, time.Now().UTC().Format(time.RFC3339))
	_ = a.metaSet(metaRegionNote, "用户手动选择")
	return ""
}

// LocateFromCoords 用浏览器交来的经纬度做离线就近判定，并保存为当前地区
// （source=auto）。误差以距离形式如实回传，UI 必须展示并允许更正。
func (a *App) LocateFromCoords(lat, lng float64) LocateResult {
	r, d := geo.Nearest(lat, lng)
	if r.Code == "" {
		return LocateResult{OK: false, Msg: fmt.Sprintf("坐标无效（lat=%v lng=%v），未改动当前地区", lat, lng)}
	}
	note := fmt.Sprintf("按最近行政中心判定，距中心约 %.0f km，请核对", d)
	if d > 60 {
		note = fmt.Sprintf("该位置距最近行政中心约 %.0f km，判定可靠性低，请手动核对或改用手动选择", d)
	}
	if err := a.metaSet(metaRegion, r.Code); err != nil {
		return LocateResult{OK: false, Msg: err.Error()}
	}
	_ = a.metaSet(metaRegionSource, "auto")
	_ = a.metaSet(metaRegionAt, time.Now().UTC().Format(time.RFC3339))
	_ = a.metaSet(metaRegionNote, note)
	return LocateResult{
		OK:  true,
		Msg: note,
		Region: RegionState{
			Code: r.Code, Name: r.Name, Path: geo.PathName(r.Code),
			Source: "auto", Note: note, DistanceKm: d,
		},
	}
}

// regionPatterns 地区码 → 名称前缀集合。选省级时必须连同其下辖地级一并纳入，
// 否则「石家庄市…」这类法规会被「河北省」漏掉。
func regionPatterns(code string) []string {
	c := strings.TrimSpace(code)
	if c == "" {
		return nil
	}
	r, ok := geo.ByCode(c)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	add(r.Name)
	for _, al := range geo.AliasesOf(r.Code) { // 「深圳经济特区…」这类命名同样归入本地区
		add(al)
	}
	if r.Level == geo.LevelCity {
		return out // 地级：只认自身与自身别名，不牵连同省其他市
	}
	for _, ch := range geo.Children(r.Code) {
		add(ch.Name)
		for _, al := range geo.AliasesOf(ch.Code) {
			add(al)
		}
	}
	return out
}

// escapeLike 转义 LIKE 通配符（配合 SQL 的 ESCAPE '\'）。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// regionClause 构造地域过滤子句与参数（patterns 空 = 不过滤）。
func regionClause(patterns []string) (string, []any) {
	if len(patterns) == 0 {
		return "", nil
	}
	var sb strings.Builder
	args := make([]any, 0, len(patterns))
	sb.WriteString(" AND (")
	for i, p := range patterns {
		if i > 0 {
			sb.WriteString(" OR ")
		}
		sb.WriteString("l.name LIKE ? ESCAPE '\\'")
		args = append(args, escapeLike(p)+"%")
	}
	sb.WriteString(")")
	return sb.String(), args
}

// lawRegion 单条法规的推导地域（UI 显示；空串 = 地域未标注）。
func lawRegion(name string) string {
	return parse.RegionOf(name)
}
