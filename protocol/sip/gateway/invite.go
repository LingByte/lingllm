package gateway

import (
	"fmt"
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/dialog"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/LingByte/lingllm/protocol/sip/uas"
)

// PickCodec chooses the first codec from offer matching a preferred name list.
// When no preference matches, the first offered codec is returned.
func PickCodec(offer *sdp.Info, prefer ...string) (sdp.Codec, bool) {
	if offer == nil || len(offer.Codecs) == 0 {
		return sdp.Codec{}, false
	}
	if len(prefer) == 0 {
		prefer = []string{"pcma", "pcmu", "g722", "opus"}
	}
	for _, name := range prefer {
		for _, c := range offer.Codecs {
			if strings.EqualFold(c.Name, name) {
				return c, true
			}
		}
	}
	return offer.Codecs[0], true
}

// InviteAnswer builds a 200 OK INVITE response with an SDP answer and UAS To tag.
// localSIPPort is the signaling Contact port (defaults to 5060 when <= 0).
func InviteAnswer(req *stack.Message, localIP string, localSIPPort, localRTPPort int, codec sdp.Codec, localTag string) (*stack.Message, *dialog.Dialog, error) {
	if req == nil || !req.IsRequest || req.Method != stack.MethodInvite {
		return nil, nil, fmt.Errorf("sip/gateway: need INVITE request")
	}
	if strings.TrimSpace(localTag) == "" {
		localTag = NewTag()
	}
	dlg, err := dialog.NewUASFromINVITE(req)
	if err != nil {
		return nil, nil, err
	}
	dlg.SetLocalTag(localTag)

	offer, err := sdp.Parse(req.Body)
	if err != nil {
		return nil, nil, err
	}
	proto := strings.TrimSpace(offer.Proto)
	if proto == "" {
		proto = "RTP/AVP"
	}
	body := sdp.GenerateWithProto(localIP, localRTPPort, proto, []sdp.Codec{codec})

	resp, err := uas.NewResponse(req, 200, "OK", body, "application/sdp")
	if err != nil {
		return nil, nil, err
	}
	to := dialog.AppendTagAfterNameAddr(req.GetHeader(stack.HeaderTo), localTag)
	resp.SetHeader(stack.HeaderTo, to)
	resp.SetHeader(stack.HeaderContact, uas.FormatContact(localIP, localSIPPort, ""))
	return resp, dlg, nil
}

// Ringing builds a 180 Ringing provisional response with UAS To tag.
func Ringing(req *stack.Message, localTag string) (*stack.Message, error) {
	if strings.TrimSpace(localTag) == "" {
		localTag = NewTag()
	}
	resp, err := uas.NewResponse(req, 180, "Ringing", "", "")
	if err != nil {
		return nil, err
	}
	to := dialog.AppendTagAfterNameAddr(req.GetHeader(stack.HeaderTo), localTag)
	resp.SetHeader(stack.HeaderTo, to)
	return resp, nil
}
