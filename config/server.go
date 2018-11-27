package config

import (
	"overlord/lib/log"
)

// ServerConfig is apiserver's config
type ServerConfig struct {
	Listen string `toml:"listen"`
	Etcd   string `toml:"etcd"`
	*log.Config
}
