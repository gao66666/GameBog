//go:build ignore
// +build ignore

package main

// 研发笔记（仅用于记录思路，不参与编译）
// 1. 点赞/阅读量统计链路（Redis 实时 + NSQ 批量落库）
// 2. 搜索链路（写入触发同步 + Kafka + 查询走 Elasticsearch）
// 3. WebSocket 通知链路（Kafka）