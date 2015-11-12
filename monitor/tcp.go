package monitor

import (
	"net"
	"time"

	"github.com/ch3lo/yale/util"
)

type TcpMonitor struct {
	request  string
	expected string
	retries  int
}

func (tcp *TcpMonitor) Check(addr string) bool {

	try := 1
	for tcp.retries == -1 || try <= tcp.retries {
		util.Log.Infof("TCP Check intento %d/%d", try, tcp.retries)
		conn, err := net.Dial("tcp", addr)

		if err == nil {
			util.Log.Infoln("Se recibió respuesta del servidor", addr)
			conn.Close()
			return true
		} else {
			util.Log.Debugln(err)
		}

		try++
		time.Sleep(10 * 1e9)
	}

	return false
}

func (tcp *TcpMonitor) SetRequest(ep string) {
	tcp.request = ep
}

func (tcp *TcpMonitor) SetExpected(ex string) {
	tcp.expected = ex
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
