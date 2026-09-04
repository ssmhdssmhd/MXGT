package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// writeYAML 将配置序列化为 yaml 写入文件（首次运行自动生成）
func writeYAML(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
