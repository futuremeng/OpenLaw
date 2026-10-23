package sdk

// 错误码与处理建议（开发文档 5.4）
const (
	CodeLawNotFound     = "OL-1001" // 未找到法规 → 提示核对名称与简称
	CodeNoVersionAtDate = "OL-1002" // 指定日期无生效版本 → 提示确认时间范围
	CodeArticleNotFound = "OL-1003" // 条款号不存在 → 提示核对条款号与版本
	CodeQuoteMismatch   = "OL-1004" // 引用内容与原文差异大 → 展示原文供人工比对
	CodeVersionExpired  = "OL-1005" // 版本已失效 → 返回当前有效版本
	CodeBrokenChain     = "OL-1006" // 溯源断链 → 禁止输出结论，提示人工核对
	CodeHashFail        = "OL-1007" // 哈希校验失败 → 立即告警并隔离文件
	CodeSignatureFail   = "OL-1008" // manifest 签名验证失败 → 停止使用数据目录
	CodeOCRLowConfid    = "OL-1009" // OCR 置信度过低 → 转人工确认
)

// ErrorHints 错误码 → 界面提示文案建议（12.3 结果处理规则）。
var ErrorHints = map[string]string{
	CodeLawNotFound:     "未找到该法规，请核对名称与简称",
	CodeNoVersionAtDate: "指定日期无生效版本，请确认时间范围",
	CodeArticleNotFound: "条款号不存在，请核对条款号与版本",
	CodeQuoteMismatch:   "引用内容与原文差异较大，可能引用错误，已展示原文供人工比对",
	CodeVersionExpired:  "该版本在给定日期后已失效，建议核对现行版本",
	CodeBrokenChain:     "无法溯源，禁止输出判错结论，请人工核对公报原文",
	CodeHashFail:        "哈希校验失败，文件可能已被替换，请立即告警并隔离",
	CodeSignatureFail:   "manifest 签名验证失败，停止使用数据目录",
	CodeOCRLowConfid:    "OCR 置信度过低，已转人工确认",
}
