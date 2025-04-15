package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Title    string
	SubTitle string
	Desc     string
	Author   string
}

func Load(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}
	}

	var cfg Config
	if err = json.Unmarshal(data, &cfg); err != nil {
		return Config{}
	} else {
		return cfg
	}
}
