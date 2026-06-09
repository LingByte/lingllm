package transfer

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

// DialogState holds in-dialog routing data for NOTIFY/BYE on an inbound UAS leg.
type DialogState struct {
	Remote     *net.UDPAddr
	From       string // our To from 200 OK (local tag)
	To         string // remote From from INVITE
	RequestURI string
	NextCSeq   int
}

// DialogStore tracks confirmed inbound dialogs keyed by Call-ID.
type DialogStore struct {
	mu   sync.Mutex
	byID map[string]*DialogState
}

// NewDialogStore creates an empty dialog map.
func NewDialogStore() *DialogStore {
	return &DialogStore{byID: make(map[string]*DialogState)}
}

// Remember records dialog state after answering INVITE (pass the To header from 200 OK).
func (s *DialogStore) Remember(callID string, remote *net.UDPAddr, inv *stack.Message, ourToWithTag string) {
	if s == nil || callID == "" || inv == nil || remote == nil {
		return
	}
	reqURI := requestURIFromContact(inv.GetHeader(stack.HeaderContact))
	if reqURI == "" {
		reqURI = strings.TrimSpace(inv.RequestURI)
	}
	st := &DialogState{
		Remote:     cloneUDP(remote),
		From:       strings.TrimSpace(ourToWithTag),
		To:         inv.GetHeader(stack.HeaderFrom),
		RequestURI: reqURI,
		NextCSeq:   parseInviteCSeqNext(inv.GetHeader(stack.HeaderCSeq)),
	}
	s.mu.Lock()
	if s.byID == nil {
		s.byID = make(map[string]*DialogState)
	}
	s.byID[normCallID(callID)] = st
	s.mu.Unlock()
}

// Forget removes dialog state (after BYE or teardown).
func (s *DialogStore) Forget(callID string) {
	if s == nil {
		return
	}
	callID = normCallID(callID)
	s.mu.Lock()
	delete(s.byID, callID)
	s.mu.Unlock()
}

// Get returns dialog state for callID.
func (s *DialogStore) Get(callID string) *DialogState {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[normCallID(callID)]
}

// BuildNotify builds an in-dialog NOTIFY (Event: refer) with message/sipfrag body.
func (s *DialogStore) BuildNotify(callID, localIP string, localPort int, sipfragBody, subscriptionState string) (*stack.Message, *net.UDPAddr, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("sip/transfer: nil dialog store")
	}
	callID = normCallID(callID)
	s.mu.Lock()
	d := s.byID[callID]
	if d == nil {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("sip/transfer: no dialog for call-id")
	}
	if strings.TrimSpace(d.From) == "" || strings.TrimSpace(d.To) == "" || d.Remote == nil {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("sip/transfer: incomplete dialog")
	}
	cseq := d.NextCSeq
	d.NextCSeq++
	from, to, reqURI, remote := d.From, d.To, d.RequestURI, cloneUDP(d.Remote)
	s.mu.Unlock()

	if strings.TrimSpace(subscriptionState) == "" {
		subscriptionState = "active;expires=60"
	}
	branch := randomHexBranch()
	via := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport",
		strings.TrimSpace(localIP), localPort, branch)
	msg := &stack.Message{
		IsRequest:  true,
		Method:     stack.MethodNotify,
		RequestURI: reqURI,
		Version: stack.SIPVersion,
	}
	msg.SetHeader(stack.HeaderVia, via)
	msg.SetHeader(stack.HeaderMaxForwards, stack.DefaultMaxForwards)
	msg.SetHeader(stack.HeaderFrom, from)
	msg.SetHeader(stack.HeaderTo, to)
	msg.SetHeader(stack.HeaderCallID, callID)
	msg.SetHeader(stack.HeaderCSeq, fmt.Sprintf("%d NOTIFY", cseq))
	msg.SetHeader(stack.HeaderEvent, "refer")
	msg.SetHeader(stack.HeaderSubscriptionState, subscriptionState)
	msg.SetHeader(stack.HeaderContentType, "message/sipfrag;version=2.0")
	msg.Body = strings.TrimRight(strings.TrimSpace(sipfragBody), "\r\n") + "\r\n"
	msg.SetHeader(stack.HeaderContentLength, strconv.Itoa(stack.BodyBytesLen(msg.Body)))
	return msg, remote, nil
}

// BuildBye builds an in-dialog BYE for the inbound leg.
func (s *DialogStore) BuildBye(callID, localIP string, localPort int) (*stack.Message, *net.UDPAddr, error) {
	if s == nil {
		return nil, nil, fmt.Errorf("sip/transfer: nil dialog store")
	}
	callID = normCallID(callID)
	s.mu.Lock()
	d := s.byID[callID]
	if d == nil {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("sip/transfer: no dialog for call-id")
	}
	if strings.TrimSpace(d.From) == "" || strings.TrimSpace(d.To) == "" || d.Remote == nil {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("sip/transfer: incomplete dialog")
	}
	cseq := d.NextCSeq
	d.NextCSeq++
	from, to, reqURI, remote := d.From, d.To, d.RequestURI, cloneUDP(d.Remote)
	s.mu.Unlock()

	branch := randomHexBranch()
	via := fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport",
		strings.TrimSpace(localIP), localPort, branch)
	msg := &stack.Message{
		IsRequest:  true,
		Method:     stack.MethodBye,
		RequestURI: reqURI,
		Version: stack.SIPVersion,
	}
	msg.SetHeader(stack.HeaderVia, via)
	msg.SetHeader(stack.HeaderMaxForwards, stack.DefaultMaxForwards)
	msg.SetHeader(stack.HeaderFrom, from)
	msg.SetHeader(stack.HeaderTo, to)
	msg.SetHeader(stack.HeaderCallID, callID)
	msg.SetHeader(stack.HeaderCSeq, fmt.Sprintf("%d BYE", cseq))
	msg.SetHeader(stack.HeaderContentLength, "0")
	return msg, remote, nil
}

func cloneUDP(a *net.UDPAddr) *net.UDPAddr {
	if a == nil {
		return nil
	}
	b := *a
	return &b
}

func parseInviteCSeqNext(cseq string) int {
	cseq = strings.TrimSpace(cseq)
	if cseq == "" {
		return 2
	}
	parts := strings.Fields(cseq)
	if len(parts) == 0 {
		return 2
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n < 1 {
		return 2
	}
	return n + 1
}

func requestURIFromContact(contact string) string {
	contact = strings.TrimSpace(contact)
	if contact == "" {
		return ""
	}
	c := contact
	if i := strings.Index(c, "<"); i >= 0 {
		c = c[i+1:]
	}
	if i := strings.Index(c, ">"); i >= 0 {
		c = c[:i]
	}
	c = strings.TrimSpace(c)
	if i := strings.Index(c, ";"); i > 0 {
		c = c[:i]
	}
	c = strings.TrimSpace(c)
	if c == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(c), "sip:") {
		c = "sip:" + c
	}
	return c
}

func normCallID(s string) string { return strings.TrimSpace(s) }

func randomHexBranch() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "deadbeef01"
	}
	return hex.EncodeToString(b)
}
