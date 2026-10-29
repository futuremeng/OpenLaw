// Package geo 中国行政区划离线词表与就近判定（OL-12 地点定位）。
//
// 纪律：默认离线——本包不联网、不调用任何地理编码服务，全部判定基于内置数据。
// 内置数据为自行整理的行政区划表与**近似中心点**（非官方测绘成果，误差可达
// 0.1°~0.3°，约 10~30 km），只用于「就近判定到地级行政区」这一件事；边界附近
// 可能判错，因此上层必须如实标注误差并允许用户手动更正（诚实展示纪律）。
//
// 名称推导（法规名称 → 地域）见 parse/regioninfer.go，它复用本包的词表。
package geo

import (
	"math"
	"sort"
	"strings"
)

// 行政层级。
const (
	LevelProvince = 1 // 省级（省/自治区/直辖市/特别行政区）
	LevelCity     = 2 // 地级（地级市/自治州/盟/地区/直辖市辖区）
)

// Region 一个行政区划条目。Code 采用 GB/T 2260 风格的 6 位数字码。
type Region struct {
	Code   string  `json:"code"`
	Name   string  `json:"name"`   // 北京市 / 石家庄市 / 湘西土家族苗族自治州
	Parent string  `json:"parent"` // 上级 Code（省级为空）
	Level  int     `json:"level"`
	Lat    float64 `json:"lat"` // 近似中心点（纬度，十进制度）
	Lng    float64 `json:"lng"` // 近似中心点（经度，十进制度）
}

// byCode / provinces / cities / children 为内置数据的索引，init 时构建。
var (
	byCode    = map[string]*Region{}
	provinces []*Region
	cities    []*Region
	children  = map[string][]*Region{}
)

func init() {
	for i := range regions {
		r := &regions[i]
		byCode[r.Code] = r
		switch r.Level {
		case LevelProvince:
			provinces = append(provinces, r)
		case LevelCity:
			cities = append(cities, r)
			if r.Parent != "" {
				children[r.Parent] = append(children[r.Parent], r)
			}
		}
	}
	for k := range children {
		sort.Slice(children[k], func(i, j int) bool { return children[k][i].Name < children[k][j].Name })
	}
}

// ByCode 按行政区划码取条目；不存在返回 (zero, false)。
func ByCode(code string) (Region, bool) {
	c := strings.TrimSpace(code)
	if c == "" {
		return Region{}, false
	}
	if r, ok := byCode[c]; ok {
		return *r, true
	}
	return Region{}, false
}

// Provinces 全部省级行政区（按 Code 升序）。
func Provinces() []Region {
	out := make([]Region, 0, len(provinces))
	for _, r := range provinces {
		out = append(out, *r)
	}
	return out
}

// Children 某省的下一级（地级）行政区；code 非法返回 nil。
func Children(code string) []Region {
	cs := children[strings.TrimSpace(code)]
	out := make([]Region, 0, len(cs))
	for _, r := range cs {
		out = append(out, *r)
	}
	return out
}

// Path 该行政区的层级路径（省 → 市），用于 UI 面包屑；code 非法返回 nil。
func Path(code string) []Region {
	r, ok := ByCode(code)
	if !ok {
		return nil
	}
	var out []Region
	if r.Level == LevelCity && r.Parent != "" {
		if p, ok := byCode[r.Parent]; ok {
			out = append(out, *p)
		}
	}
	out = append(out, r)
	return out
}

// PathName 「河北省 · 石家庄市」这样的可读路径；code 非法返回空串。
func PathName(code string) string {
	p := Path(code)
	if len(p) == 0 {
		return ""
	}
	names := make([]string, 0, len(p))
	for _, r := range p {
		names = append(names, r.Name)
	}
	return strings.Join(names, " · ")
}

// NameOf 行政区名；code 非法返回空串。
func NameOf(code string) string {
	if r, ok := ByCode(code); ok {
		return r.Name
	}
	return ""
}

// ValidCode 是否为已知行政区码（空串视为「不限地区」，同样合法）。
func ValidCode(code string) bool {
	c := strings.TrimSpace(code)
	return c == "" || func() bool { _, ok := byCode[c]; return ok }()
}

// haversineKm 两点间大圆距离（km）。
func haversineKm(a, b Region) float64 {
	const R = 6371.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := rad(b.Lat - a.Lat)
	dLng := rad(b.Lng - a.Lng)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * R * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Nearest 按经纬度就近判定到地级行政区（无地级数据时退到省级）。
// 返回：判定到的行政区、其中心点距离 km、以及该点与中心的距离 km。
// 距离本身即置信提示——UI 必须把它如实展示给用户核对，不得当作确定结论。
func Nearest(lat, lng float64) (Region, float64) {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return Region{}, -1
	}
	pt := Region{Lat: lat, Lng: lng}
	best, bestD := Region{}, math.MaxFloat64
	pool := cities
	if len(pool) == 0 {
		pool = provinces
	}
	for _, r := range pool {
		d := haversineKm(pt, *r)
		if d < bestD {
			best, bestD = *r, d
		}
	}
	if best.Code == "" {
		return Region{}, -1
	}
	return best, bestD
}

// regionAliases 命名别名前缀 → 区划码。部分法规以「××经济特区」而非市名开头
// （深圳/厦门/珠海/汕头/海南经济特区），只认市名会漏；别名一律保守列举，
// 不确定的一律不列（判不出就留空）。
var regionAliases = []struct {
	prefix string
	code   string
}{
	{"深圳经济特区", "440300"},
	{"厦门经济特区", "350200"},
	{"珠海经济特区", "440400"},
	{"汕头经济特区", "440500"},
	{"海南经济特区", "460000"},
}

// AliasesOf 该区划码在名称里可能出现的别名前缀（过滤与推导共用，保证口径一致）。
func AliasesOf(code string) []string {
	c := strings.TrimSpace(code)
	if c == "" {
		return nil
	}
	var out []string
	for _, a := range regionAliases {
		if a.code == c {
			out = append(out, a.prefix)
		}
	}
	return out
}

// MatchPrefix 按名称前缀匹配已知行政区，返回最长匹配；同级并列时取上级
// （「吉林省吉林市…」判省级，与 parse/levelinfer.go 先强后弱的顺序一致）。
// 别名前缀（「深圳经济特区…」）同样参与匹配，命中后归到其所属行政区。
// 无匹配返回 (zero, false)。
func MatchPrefix(name string) (Region, bool) {
	n := strings.TrimSpace(name)
	if n == "" {
		return Region{}, false
	}
	var best *Region
	bestLen := 0
	better := func(name string, level int) bool {
		return len(name) > bestLen || (len(name) == bestLen && best != nil && level < best.Level)
	}
	for i := range regions {
		r := &regions[i]
		if !strings.HasPrefix(n, r.Name) {
			continue
		}
		if best == nil || better(r.Name, r.Level) {
			best, bestLen = r, len(r.Name)
		}
	}
	for _, a := range regionAliases {
		if !strings.HasPrefix(n, a.prefix) {
			continue
		}
		r, ok := byCode[a.code]
		if !ok {
			continue
		}
		if best == nil || better(a.prefix, r.Level) {
			best, bestLen = r, len(a.prefix)
		}
	}
	if best == nil {
		return Region{}, false
	}
	return *best, true
}

// Count 内置条目总数（测试与自检用）。
func Count() int { return len(regions) }
