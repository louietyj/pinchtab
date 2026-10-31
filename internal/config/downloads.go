package config

// Download behaviors for instanceDefaults.downloadBehavior.
const (
	// DownloadManaged saves every download under a guid name in a directory
	// pinchtab owns, to be copied back to the caller.
	DownloadManaged = "managed"
	// DownloadDefault leaves where and how a download is saved to the browser
	// (its folder, renaming, save prompts) and only follows the events: for an
	// attached personal Chrome. A headless browser cancels downloads this way.
	DownloadDefault = "default"
	// DownloadOff leaves the download manager alone; downloads are not tracked.
	DownloadOff = "off"
)

// DefaultDownloadFileMaxBytes caps one page download. It is far above the
// in-memory cap on `download <url>`, since the file goes to disk.
const DefaultDownloadFileMaxBytes = 1 << 30

// EffectiveDownloadBehavior resolves an unset behavior: a browser pinchtab
// launched is its own, while an attached one may be the user's.
func (cfg *RuntimeConfig) EffectiveDownloadBehavior(attached bool) string {
	if cfg != nil && cfg.DownloadBehavior != "" {
		return cfg.DownloadBehavior
	}
	if attached {
		return DownloadDefault
	}
	return DownloadManaged
}

func (cfg *RuntimeConfig) EffectiveDownloadFileMaxBytes() int {
	if cfg == nil || cfg.DownloadFileMaxBytes <= 0 {
		return DefaultDownloadFileMaxBytes
	}
	return cfg.DownloadFileMaxBytes
}
