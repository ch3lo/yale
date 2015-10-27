package monitor

type Monitor interface {
	Check(addr string) bool
	SetEndpoint(ep string)
	SetExpect(ex string)
	SetRetries(retries int)
}
