package stack

// RequestMethod returns e.Request.Method for request-bearing events, or "".
func (e Event) RequestMethod() string {
	if e.Request == nil {
		return ""
	}
	return e.Request.Method
}

// ResponseStatus returns e.Response.StatusCode for response-bearing events, or 0.
func (e Event) ResponseStatus() int {
	if e.Response == nil {
		return 0
	}
	return e.Response.StatusCode
}
