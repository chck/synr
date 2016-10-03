package config

import(
  "io/ioutil"
  "log"

  "gopkg.in/yaml.v2"
)

type Config struct {
  Tokens struct {
    Chatwork string
    Slack string
  }
}

func Load() Config {
  file, err := ioutil.ReadFile("./config/secrets.yaml")

  if err != nil {
    log.Fatal("failed to read config file")
  }

  config := &Config{}
  _ = yaml.Unmarshal([]byte(file), config)
  return *config
}

