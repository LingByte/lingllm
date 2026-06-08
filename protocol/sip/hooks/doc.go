// Package hooks defines optional callbacks for call lifecycle and recording delivery.
//
// No database or ORM types live here — applications implement these interfaces
// (GORM, object storage, CDR export, etc.) and wire them at gateway/session setup.
//
// Recording split:
//   - protocol/sipmedia/session keeps lightweight in-memory SN3 RTP capture (RTP timing,
//     transfer-bridge paths). That is SIP/RTP plumbing, not business recording.
//   - Stereo PCM export, WAV/MP3 encoding, and durable storage are implemented via
//     RecordingSink in protocol/voice or the application layer.
package hooks
