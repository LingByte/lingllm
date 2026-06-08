package transaction

// Copyright (c) 2026 LinByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

// Observability hook for the transaction layer.
//
// We deliberately keep this in a separate file (rather than inlining
// the metric call at the timeout site) so the transaction package
// can be tested without dragging in the metrics registry. If a
// future test wants to assert "no metrics were touched" it can
// rebind onTransactionTimeout to a no-op via SetTransactionTimeoutHook.

import (
	sipMetrics "github.com/LingByte/lingllm/protocol/sip/metrics"
)

// onTransactionTimeout is the actual call site emitter. Kept as a
// var (not a const) so tests can swap it out. Production never
// changes it.
var onTransactionTimeout = func(method string) {
	sipMetrics.TransactionTimeout(method)
}

// SetTransactionTimeoutHook lets tests intercept timer-B/F firings
// without touching the metrics registry. Pass nil to reset to the
// default sipMetrics-backed emitter.
func SetTransactionTimeoutHook(fn func(method string)) {
	if fn == nil {
		onTransactionTimeout = func(method string) {
			sipMetrics.TransactionTimeout(method)
		}
		return
	}
	onTransactionTimeout = fn
}
