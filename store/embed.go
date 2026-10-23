package store

import _ "embed"

// schemaSQL 内嵌完整建表语句（13.1 + 13.2），随二进制发布，不依赖运行时文件路径。
//
//go:embed schema.sql
var schemaSQL string
