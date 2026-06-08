package transfer

import (
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/historyinfo"
	"github.com/LingByte/lingllm/protocol/sip/outbound"
)

// ApplyRetargetHeaders mutates req to include History-Info and Diversion chains for B2BUA retarget.
func ApplyRetargetHeaders(
	req *outbound.DialRequest,
	rawTo, rawHistoryInfo, rawDiversion string,
	historyReason, diversionReason string,
) {
	if req == nil {
		return
	}
	hi, dv := buildRetargetHeaders(rawTo, rawHistoryInfo, rawDiversion, req.Target.RequestURI, historyReason, diversionReason)
	if len(hi) > 0 {
		req.HistoryInfo = hi
	}
	if len(dv) > 0 {
		req.Diversion = dv
	}
}

func buildRetargetHeaders(
	rawTo, rawHistoryInfo, rawDiversion string,
	newTargetURI string,
	historyReason string,
	diversionReason string,
) ([]historyinfo.Entry, []historyinfo.Diversion) {
	newTargetURI = strings.TrimSpace(newTargetURI)
	originalTo := historyinfo.ExtractURIFromToHeader(rawTo)
	if newTargetURI == "" {
		return nil, nil
	}
	inboundHistory := historyinfo.ParseChain(rawHistoryInfo)
	inboundDiversion := historyinfo.ParseDiversionChain(rawDiversion)
	if originalTo == "" && len(inboundHistory) == 0 && len(inboundDiversion) == 0 {
		return nil, nil
	}
	hi := historyinfo.AppendTransferEntry(inboundHistory, originalTo, newTargetURI, historyReason)
	dv := historyinfo.AppendDiversionEntry(inboundDiversion, originalTo, diversionReason)
	return hi, dv
}
