# Voice Demo

End-to-end examples for voice conversation with LingLLM.

## Layout

| Path | Role |
|------|------|
| `dialogue/` | Dialog WebSocket server; streams LLM replies as `tts.stream` (pipeline) |
| `voice/` | Full media server: WebRTC + xiaozhi (pipeline or realtime) |
| `realtime/` | Realtime-only server + `web1` browser client |
| `voiceutil/` | Shared ASR/TTS/realtime factory (used by `voice/` and `realtime/`) |
| `web/index.html` | Browser client — **WebRTC pipeline** (ASR → LLM → TTS) |
| `web1/index.html` | Browser client — **xiaozhi WS realtime** (multimodal agent) |

## Pipeline mode (ASR → LLM → TTS)

Terminal 1 — dialog app:

```bash
export OPENAI_API_KEY=sk-...
go run ./examples/voice-demo/dialogue -provider openai -model gpt-4o-mini
```

Terminal 2 — voice server:

```bash
export ASR_PROVIDER=volcengine
export ASR_CONFIG_JSON='{"app_id":"...","token":"...","cluster":"..."}'

export TTS_PROVIDER=openai
export TTS_CONFIG_JSON='{"apiKey":"'"$OPENAI_API_KEY"'","voice":"alloy"}'

go run -tags opus ./examples/voice-demo/voice -dialog ws://127.0.0.1:8082/ws/dialog
```

**WebRTC 必须带 `-tags opus`**。Open [http://localhost:8080/](http://localhost:8080/) (`web/`), click **Connect**, and speak.

## Realtime mode (multimodal agent)

No dialogue server, no ASR/TTS. One process serves `web1` and xiaozhi WebSocket.

### Aliyun Qwen-Omni

```bash
export REALTIME_CONFIG_JSON='{
  "provider": "aliyun_omni",
  "api_key": "sk-...",
  "model": "qwen3.5-omni-flash-realtime-2026-03-15"
}'
export REALTIME_VOICE=Cherry
export REALTIME_SYSTEM_PROMPT="You are a helpful voice assistant."

go run ./examples/voice-demo/realtime
open http://localhost:8080/
```

### Volcengine realtime dialogue

```bash
export REALTIME_CONFIG_JSON='{
  "provider": "volcengine_dialogue",
  "appId": "...",
  "accessKey": "..."
}'
go run ./examples/voice-demo/realtime
```

Browser: open [http://localhost:8080/](http://localhost:8080/) and click **Connect** (`web1/`).

Xiaozhi device: `ws://host:8080/xiaozhi/v1/`

Alternative — use the full `voice` binary with `-mode realtime -web ../web1` (also exposes WebRTC, which still needs pipeline credentials).

## Environment variables

| Variable | Used by | Description |
|----------|---------|-------------|
| `OPENAI_API_KEY` | dialogue, TTS | LLM / OpenAI TTS |
| `ASR_PROVIDER` | voice (pipeline) | e.g. `volcengine`, `tencent` |
| `ASR_CONFIG_JSON` | voice (pipeline) | Provider credential JSON |
| `TTS_PROVIDER` | voice (pipeline) | e.g. `openai`, `tencent` |
| `TTS_CONFIG_JSON` | voice (pipeline) | Provider credential JSON |
| `REALTIME_CONFIG_JSON` | realtime, voice | `realtime.NewAgentFromCredential` config |
| `REALTIME_SYSTEM_PROMPT` | realtime | Agent instructions |
| `REALTIME_VOICE` | realtime | Voice id (provider-specific) |
| `REALTIME_INPUT_SR` | realtime | Uplink PCM rate (default 16000) |
| `REALTIME_OUTPUT_SR` | realtime | Agent output rate (default 24000) |

Registered realtime providers: `aliyun_omni`, `volcengine_dialogue` (see `realtime/` package).

## Architecture

```
web/     Browser ── WebRTC ──► voice (webrtc) ── dialog WS ──► dialogue (LLM)
web1/    Browser ── xiaozhi WS ──► realtime server ──► multimodal agent
Device   ── xiaozhi WS ──► voice/realtime ──► agent or dialog pipeline
```
