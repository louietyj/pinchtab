# Record

Record browser activity as a video file. Supports GIF (pure Go), WebM, and MP4 (require ffmpeg).

```bash
# Start recording
curl -X POST http://localhost:9867/record/start \
  -H "Content-Type: application/json" \
  -d '{"format":"gif","fps":5,"quality":80}'

# Check status
curl http://localhost:9867/record/status

# Stop: encoding runs in the background into <stateDir>/recordings/
curl -X POST http://localhost:9867/record/stop -d '{}'
# {"status":"encoding","path":"<stateDir>/recordings/rec_<timestamp>.gif","format":"gif","frames":62,"hint":"..."}

# Stop without encoding
curl -X POST http://localhost:9867/record/stop -d '{"discard":true}'
# {"status":"discarded","format":"gif","frames":62}
```

The server chooses the output path; poll `/record/status` until `state` is
`finished` (API flow exercised in `tests/e2e/scenarios/api/recording-smoke.sh`).

## Start Response

```json
{
  "status": "recording",
  "format": "gif",
  "fps": 5,
  "quality": 80,
  "tabId": "tab1"
}
```

## Status Response

```json
{
  "active": true,
  "state": "recording",
  "format": "gif",
  "durationSeconds": 12.5,
  "frames": 62,
  "tabId": "tab1",
  "fps": 5
}
```

`state` is one of `idle`, `recording`, `limit_reached`, `stopping`, `encoding`,
`finished`, `aborted`. While a recording exists the body may also carry
`stopReason` and `outputPath`; once `finished` it carries `outputPath`, and
`error` or `warning` when encoding failed or truncated.

## API Body Fields (POST /record/start)

- `format`: `gif` (default), `webm`, or `mp4`. Determined by file extension in CLI. Anything else is `400 invalid_format`.
- `fps`: Frames per second, capped at 30 (default 5).
- `quality`: JPEG capture quality, capped at 100 (default 80).
- `scale`: Resolution multiplier, capped at 1.0 (default 1.0). Values < 1 reduce output size.
- `tabId`: Target a specific tab.

A second `start` while a recording is active returns `409 recording_error`;
`stop` with no active recording returns `400 recording_error`.

## API Body Fields (POST /record/stop)

- `discard`: `true` to drop the frames without encoding.

## CLI

- `record start <file>`: Start recording. Format from extension (.gif, .webm, .mp4).
- `record stop`: Stop, wait for the server to finish encoding, then move the file to the path given at start (`recording-<timestamp>.gif` if none was recorded).
- `record status`: Show active recording info.
- `--fps <n>`: Frames per second (default 5).
- `--quality <n>`: JPEG capture quality (default 80).
- `--scale <f>`: Resolution scale (default 1.0).
- `--tab <id>`: Target a specific tab.

## Format Dependencies

| Format | Dependency | Encoding | Notes |
| --- | --- | --- | --- |
| `.gif` | None | Pure Go (Floyd-Steinberg dithering) | Always available; CPU-intensive for long recordings |
| `.webm` | `ffmpeg` | VP8 via ffmpeg pipe | Requires ffmpeg on `$PATH` |
| `.mp4` | `ffmpeg` | H.264 via ffmpeg pipe | Requires ffmpeg on `$PATH` |

GIF encoding runs entirely in-process — no external binary needed. Dithering is CPU-bound. A GIF encodes at most 600 frames (~2 minutes at 5 fps) and at most 256 MB of in-memory frame data; frames are kept at the requested scale (no forced downscale), so larger captures fit fewer frames before truncation. Capture itself stops at 5 minutes or 9000 frames (`state: limit_reached`).

WebM and MP4 stream frames to ffmpeg after `record stop`. If ffmpeg is not installed, `record start` returns `400 ffmpeg_required`. Install ffmpeg via your package manager (`brew install ffmpeg`, `apt install ffmpeg`, etc.) or use `.gif` as a zero-dependency alternative.

## Notes

- Gated by `security.allowScreencast` (disabled by default). Enable with `pinchtab config set security.allowScreencast true` and restart the server.
- One active recording per bridge instance.
- Recordings are owned by the caller's session (or agent id); an anonymous recording can be stopped by any caller.
- MCP: `pinchtab_record` takes `action` (`start`|`stop`|`status`, required), `file`, `fps`, `quality`, `scale`, `tabId`.

## Related Pages

- [Screenshot](./screenshot.md)
- [PDF](./pdf.md)
