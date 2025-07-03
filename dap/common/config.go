package common

type Config struct {
	StopOnEntry bool `json:"stopOnEntry,omitempty"`
}

func (c Config) GetConfig() Config {
	return c
}
