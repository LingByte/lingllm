package stack

import "net"

// DefaultNoRouteResponse builds a 501 Not Implemented for an unregistered SIP method.
// It copies Via, From, To, Call-ID, and CSeq from the request per RFC 3261.
func DefaultNoRouteResponse(req *Message) *Message {
	if req == nil {
		return nil
	}
	resp := &Message{
		IsRequest:    false,
		Version:      SIPVersion,
		StatusCode:   501,
		StatusText:   "Not Implemented",
		Headers:      make(map[string]string),
		HeadersMulti: make(map[string][]string),
	}
	for _, name := range CorrelationHeaders {
		for _, v := range req.GetHeaders(name) {
			resp.AddHeader(name, v)
		}
	}
	resp.PrepareForSend()
	return resp
}

func defaultNoRouteHandler(req *Message, _ *net.UDPAddr) *Message {
	return DefaultNoRouteResponse(req)
}
