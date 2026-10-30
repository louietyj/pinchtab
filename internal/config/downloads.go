package config

// Download behaviors for instanceDefaults.downloadBehavior.
const (
	// DownloadManaged saves every download under a guid name in a directory
	// pinchtab owns, to be copied back to the caller.
	DownloadManaged = "managed"
	// DownloadUser saves under the real file name in the user's own Downloads
	// folder: for an attached personal Chrome, whose downloads should land
	// where the user expects them.
	DownloadUser = "user"
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
		return DownloadUser
	}
	return DownloadManaged
}

func (cfg *RuntimeConfig) EffectiveDownloadFileMaxBytes() int {
	if cfg == nil || cfg.DownloadFileMaxBytes <= 0 {
		return DefaultDownloadFileMaxBytes
	}
	return cfg.DownloadFileMaxBytes
}
