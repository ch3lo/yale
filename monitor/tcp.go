package monitor

import (
	"net"
	"time"

	"github.com/ch3lo/yale/util"
)

type TcpMonitor struct {
	endpoint string
	expect   string
	retries  int
}

func (tcp *TcpMonitor) Check(addr string) bool {

	try := 1
	for tcp.retries == -1 || try <= tcp.retries {
		util.Log.Infof("TCP Check attempt %d/%d", try, tcp.retries)
		conn, err := net.Dial("tcp", addr)

		if err == nil {
			util.Log.Infof("Response from %s ... OK", addr)
			conn.Close()
			return true
		} else {
			util.Log.Debugln(err)
		}

		try++
		time.Sleep(2 * 1e9)
	}

	return false
}

func (tcp *TcpMonitor) SetEndpoint(ep string) {
	tcp.endpoint = ep
}

func (tcp *TcpMonitor) SetExpect(ex string) {
	tcp.expect = ex
}

func (tcp *TcpMonitor) SetRetries(retries int) {
	tcp.retries = retries
}

func (tcp *TcpMonitor) Configured() bool {
	if tcp.retries != 0 {
		return true
	}

	return false
}
