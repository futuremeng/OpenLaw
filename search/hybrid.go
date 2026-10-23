package search

import "errors"

// ErrNotSupported 向量能力尚未接入（11 章：语义找法采用关键词与向量混合检索并做重排序）。
var ErrNotSupported = errors.New("向量混合检索尚未启用")

// HybridSearcher 混合检索接口（FR-07，后续版本实现）。
// 设计约束：向量仅做候选召回与重排序，最终展示仍必须走溯源链校验。
type HybridSearcher interface {
	// Embed 生成查询向量（依赖本地嵌入模型，默认离线禁用）。
	Embed(text string) ([]float64, error)
	// Search 关键词 + 向量混合召回，经 rerank 后返回。
	Search(query string, topK int) ([]Hit, error)
}

// Reranker 重排序接口（后续版本实现）。
type Reranker interface {
	Rerank(query string, hits []Hit) []Hit
}
