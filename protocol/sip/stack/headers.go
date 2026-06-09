package stack

// SIPVersion is the protocol version on the wire (RFC 3261).
const SIPVersion = "SIP/2.0"

// DefaultMaxForwards is the usual Max-Forwards value for new requests.
const DefaultMaxForwards = "70"

// SIP header field names in on-the-wire form (RFC 3261 and common extensions).
// Use with Message.GetHeader / SetHeader / AddHeader / GetHeaders (case-insensitive).
const (
	HeaderVia                 = "Via"
	HeaderMaxForwards         = "Max-Forwards"
	HeaderFrom                = "From"
	HeaderTo                  = "To"
	HeaderCallID              = "Call-ID"
	HeaderCSeq                = "CSeq"
	HeaderContact             = "Contact"
	HeaderAllow               = "Allow"
	HeaderSupported           = "Supported"
	HeaderUserAgent           = "User-Agent"
	HeaderContentType         = "Content-Type"
	HeaderContentLength       = "Content-Length"
	HeaderRecordRoute         = "Record-Route"
	HeaderRoute               = "Route"
	HeaderRAck                = "RAck"
	HeaderRSeq                = "RSeq"
	HeaderRequire             = "Require"
	HeaderSessionExpires      = "Session-Expires"
	HeaderMinSE               = "Min-SE"
	HeaderReferTo             = "Refer-To"
	HeaderReferredBy          = "Referred-By"
	HeaderReplaces            = "Replaces"
	HeaderPrivacy             = "Privacy"
	HeaderPAssertedIdentity   = "P-Asserted-Identity"
	HeaderHistoryInfo         = "History-Info"
	HeaderDiversion           = "Diversion"
	HeaderExpires             = "Expires"
	HeaderSubscriptionState   = "Subscription-State"
	HeaderEvent               = "Event"
	HeaderReason              = "Reason"
	HeaderAuthorization       = "Authorization"
	HeaderProxyAuthorization  = "Proxy-Authorization"
	HeaderWWWAuthenticate     = "WWW-Authenticate"
	HeaderSubject             = "Subject"
	HeaderAccept              = "Accept"
	HeaderIdentity            = "Identity" // STIR/SHAKEN
)

// CapabilityHeaders lists headers that advertise or require SIP extensions (Require, Supported).
var CapabilityHeaders = []string{HeaderRequire, HeaderSupported}

// CorrelationHeaders are copied from an inbound request when building a correlated SIP response.
var CorrelationHeaders = []string{
	HeaderVia,
	HeaderFrom,
	HeaderTo,
	HeaderCallID,
	HeaderCSeq,
}

// preferredHeaderOrder controls Message.String() emission order (canonical lowercase keys).
var preferredHeaderOrder = []string{
	canonicalHeaderKey(HeaderVia),
	canonicalHeaderKey(HeaderMaxForwards),
	canonicalHeaderKey(HeaderFrom),
	canonicalHeaderKey(HeaderTo),
	canonicalHeaderKey(HeaderCallID),
	canonicalHeaderKey(HeaderCSeq),
	canonicalHeaderKey(HeaderContact),
	canonicalHeaderKey(HeaderAllow),
	canonicalHeaderKey(HeaderSupported),
	canonicalHeaderKey(HeaderUserAgent),
	canonicalHeaderKey(HeaderContentType),
	canonicalHeaderKey(HeaderContentLength),
}

// wireHeaderNames maps canonical header keys to on-the-wire header names for serialization.
var wireHeaderNames = map[string]string{
	canonicalHeaderKey(HeaderVia):           HeaderVia,
	canonicalHeaderKey(HeaderMaxForwards):   HeaderMaxForwards,
	canonicalHeaderKey(HeaderFrom):          HeaderFrom,
	canonicalHeaderKey(HeaderTo):            HeaderTo,
	canonicalHeaderKey(HeaderCallID):        HeaderCallID,
	canonicalHeaderKey(HeaderCSeq):          HeaderCSeq,
	canonicalHeaderKey(HeaderContact):       HeaderContact,
	canonicalHeaderKey(HeaderAllow):         HeaderAllow,
	canonicalHeaderKey(HeaderSupported):     HeaderSupported,
	canonicalHeaderKey(HeaderUserAgent):     HeaderUserAgent,
	canonicalHeaderKey(HeaderContentType):   HeaderContentType,
	canonicalHeaderKey(HeaderContentLength): HeaderContentLength,
}
