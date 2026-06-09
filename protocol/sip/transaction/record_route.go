package transaction

import (
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

// RouteHeadersForDialog returns Route header field-values for a subsequent in-dialog request,
// derived from Record-Route on a dialog-creating 2xx (reverse of Record-Route appearance order).
func RouteHeadersForDialog(resp *stack.Message) []string {
	if resp == nil {
		return nil
	}
	rr := resp.GetHeaders(stack.HeaderRecordRoute)
	if len(rr) == 0 {
		return nil
	}
	out := make([]string, 0, len(rr))
	for i := len(rr) - 1; i >= 0; i-- {
		v := strings.TrimSpace(rr[i])
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
