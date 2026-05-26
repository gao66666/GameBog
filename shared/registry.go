package shared

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed tools_registry.json
var ToolsRegistryJSON []byte

// ToolRegistrySpec 与 shared/tools_registry.json 中单个工具定义对齐。
type ToolRegistrySpec struct {
	Label           string   `json:"label"`
	Group           string   `json:"group"`
	ArgKeys         []string `json:"arg_keys"`
	StartTemplate   string   `json:"start_template"`
	EndOkTemplate   string   `json:"end_ok_template"`
	EndFailTemplate string   `json:"end_fail_template"`
}

// ToolsRegistry 工具注册表（MCP + 展示模板）。
type ToolsRegistry struct {
	V            int                       `json:"v"`
	Presentation struct {
		PartialUnavailableHint string `json:"partial_unavailable_hint"`
	} `json:"presentation"`
	Tools map[string]ToolRegistrySpec `json:"tools"`
}

var (
	registryOnce sync.Once
	registryData *ToolsRegistry
	registryErr  error
)

// LoadToolsRegistry 解析嵌入的 tools_registry.json。
func LoadToolsRegistry() (*ToolsRegistry, error) {
	registryOnce.Do(func() {
		var reg ToolsRegistry
		registryErr = json.Unmarshal(ToolsRegistryJSON, &reg)
		if registryErr == nil {
			registryData = &reg
		}
	})
	return registryData, registryErr
}
