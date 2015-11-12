package monitor

import (
	"io/ioutil"
	"net/http"
	"regexp"
	"time"

	"github.com/ch3lo/yale/util"
)

type HttpMonitor struct {
	endpoint string
	expect   string
	retries  int
}

func (h *HttpMonitor) Check(addr string) bool {

	healthyEndpoint := "http://" + addr + h.endpoint

	expected, _ := regexp.Compile(".*")

	try := 1
	for h.retries == -1 || try <= h.retries {
		util.Log.Infof("HTTP Check intento %d/%d", try, h.retries)
		resp, err := http.Get(healthyEndpoint)
		if err == nil {
			util.Log.Debugf("Se recibió respuesta del servidor con estado %d", resp.StatusCode)

			if resp.StatusCode == 200 {
				util.Log.Debugln("Verificando la respuesta ...")
				body, _ := ioutil.ReadAll(resp.Body)

				result := false
				if expected.MatchString(string(body)) {
					util.Log.Infoln("Respuesta OK")
					result = true
				} else {
					util.Log.Warnf("Respuesta con error %s", string(body))
				}

				resp.Body.Close()
				return result
			}
		} else {
			util.Log.Debugln(err)
		}

		try++
		time.Sleep(10 * 1e9)
	}

	return false
}

func (http *HttpMonitor) SetEndpoint(ep string) {
	http.endpoint = ep
}

func (http *HttpMonitor) SetExpect(ex string) {
	http.expect = ex
}

func (http *HttpMonitor) SetRetries(retries int) {
	http.retries = retries
}

func (http *HttpMonitor) Configured() bool {
	if http.endpoint != "" && http.retries != 0 {
		return true
	}

	return false
}
