package monitor

import (
	"strings"
)

type MonitorType int

const (
	HTTP MonitorType = 1 + iota
	TCP
)

var monitorType = [...]string{
	"HTTP",
	"TCP",
}

func (s MonitorType) String() string {
	return monitorType[s-1]
}

func GetMonitor(t string) MonitorType {
	if strings.ToUpper(t) == TCP.String() {
		return TCP
	}

	return HTTP
}

type MonitorConfig struct {
	Type     MonitorType
	Retries  int
	Request  string
	Expected string
}

type Monitor interface {
	Check(ref string, addr string) bool
	SetRequest(ep string)
	SetExpected(ex string)
	SetRetries(retries int)
	Configured() bool
}
