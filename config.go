package servicerunner

import (
	"fmt"
	"github.com/kelseyhightower/envconfig"
	"net/http"
)

func NewConfig(name string, port int) Config {
	return NewConfigWithHost(name, "", port)
}

// NewEnvConfig generates a configuration
// by looking from environment:
//
//	SERVER_NAME: default "server"
//	SERVER_HOST: default "0.0.0.0"
//	SERVER_PORT: default "8000"
func NewEnvConfig() Config {
	var def DefaultConfig
	envconfig.MustProcess("SERVER", &def)
	return def
}

func NewConfigWithHost(name, host string, port int) Config {
	return DefaultConfig{
		ServerName: name,
		HostName:   host,
		ServerPort: port,
	}
}

// A Config represents a simple app http config.
type Config interface {
	Name() string
	Host() string
	Port() int
	addr() string
}

type DefaultConfig struct {
	ServerName string `default:"server" envconfig:"name"`
	HostName   string `default:"" envconfig:"host"`
	ServerPort int    `default:"8000" envconfig:"port"`
}

func (c DefaultConfig) Server(mu http.Handler) *http.Server {
	return &http.Server{Handler: mu, Addr: c.addr()}
}

func (c DefaultConfig) Name() string {
	return c.ServerName
}

func (c DefaultConfig) Host() string {
	return c.HostName
}

func (c DefaultConfig) Port() int {
	return c.ServerPort
}

func (c DefaultConfig) addr() string {
	return fmt.Sprintf("%s:%d", c.HostName, c.ServerPort)
}
