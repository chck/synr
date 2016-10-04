package config

import (
	"io/ioutil"
	"log"

	"gopkg.in/yaml.v2"
)

const (
	configPath = "./config/secrets.yaml"
)

type Config struct {
	Tokens struct {
		Chatwork string
		Slack    string
	}
}

func Load() Config {

	file, err := ioutil.ReadFile(configPath)

	if err != nil {
		log.Fatalf("failed to read config file, please set \"%v\" alike secrets.yaml.copy", configPath)
	}

	config := &Config{}
	_ = yaml.Unmarshal([]byte(file), config)
	return *config
}
