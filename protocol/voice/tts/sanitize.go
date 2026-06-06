package tts

import "github.com/LingByte/lingllm/utils"

// SanitizeSpeech prepares text for cloud TTS synthesis.
func SanitizeSpeech(text string) string {
	text = utils.SanitizeForSpeech(text)
	if text == "" || !utils.HasSpeakableContent(text) {
		return ""
	}
	return text
}
