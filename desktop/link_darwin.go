//go:build darwin

// wails v2.17 darwin 生产构建引用 UTType（拖拽支持）但未链接 UniformTypeIdentifiers
// 框架——在此补齐链接指令，构建不依赖外部环境变量。
//
// 必须单独成文件：带 import "C" 的文件在 CGO_ENABLED=0 下（windows 发布构建）
// 会被整个排除。若与 main.go 同文件，func main 会连带消失，链接期报
// "function main is undeclared in the main package"（v0.2.0 release run
// 44860363248 windows 失败即此因）。cgo 仅桌面壳需要；OpenLaw 核心库保持纯 Go。
package main

/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
