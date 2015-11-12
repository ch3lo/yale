package monitor

import (
	"io/ioutil"
	"net/http"
	"regexp"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/ch3lo/yale/util"
)

type HttpMonitor struct {
	request  string
	expected string
	retries  int
}

func (h *HttpMonitor) Check(ref string, addr string) bool {
	logger := util.Log.WithFields(log.Fields{
		"ds": ref,
	})

	healthyEndpoint := "http://" + addr + h.request

	expected, _ := regexp.Compile(h.expected)

	try := 1
	for h.retries == -1 || try <= h.retries {
		logger.Infof("HTTP Check intento %d/%d", try, h.retries)
		resp, err := http.Get(healthyEndpoint)
		if err == nil {
			logger.Debugf("Se recibió respuesta del servidor con estado %d", resp.StatusCode)

			if resp.StatusCode == 200 {
				logger.Debugln("Verificando la respuesta ...")
				body, _ := ioutil.ReadAll(resp.Body)

				result := false
				if expected.MatchString(string(body)) {
					logger.Infoln("Respuesta OK")
					result = true
				} else {
					logger.Warnf("Respuesta con error %s", string(body))
				}

				resp.Body.Close()
				return result
			}
		} else {
			logger.Debugln(err)
		}

		try++
		time.Sleep(10 * 1e9)
	}

	return false
}

func (http *HttpMonitor) SetRequest(ep string) {
	http.request = ep
}

func (http *HttpMonitor) SetExpected(ex string) {
	http.expected = ex
}

func (http *HttpMonitor) SetRetries(retries int) {
	http.retries = retries
}

func (http *HttpMonitor) Configured() bool {
	if http.request != "" && http.retries != 0 {
		return true
	}

	return false
}
