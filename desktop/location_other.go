//go:build !darwin

// location_other.go 非 macOS 平台：没有 CoreLocation 授权概念。
// 返回 locStatusUnsupported，由 locationPermissionView 如实说明并指向可用替代路径。
package main

func locationStatus() (int, bool, bool) { return locStatusUnsupported, false, false }

func locationRequest() (int, bool, bool) { return locStatusUnsupported, false, false }
