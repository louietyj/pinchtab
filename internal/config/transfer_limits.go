package config

const (
	MaxRetainNetworkBodyMaxBytes  = 10 << 20
	DefaultUploadMaxRequestBytes  = 10 << 20
	MaxUploadMaxRequestBytes      = 100 << 20
	DefaultUploadMaxFiles         = 8
	MaxUploadMaxFiles             = 32
	DefaultUploadMaxFileBytes     = 5 << 20
	MaxUploadMaxFileBytes         = 25 << 20
	DefaultUploadMaxTotalBytes    = 10 << 20
	MaxUploadMaxTotalBytes        = 100 << 20
	DefaultMemorySnapshotMaxBytes = 512 << 20
	MaxMemorySnapshotMaxBytes     = 4 << 30
)

func clampPositiveLimit(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func (cfg *RuntimeConfig) EffectiveUploadMaxRequestBytes() int {
	if cfg == nil {
		return DefaultUploadMaxRequestBytes
	}
	return clampPositiveLimit(cfg.UploadMaxRequestBytes, DefaultUploadMaxRequestBytes, MaxUploadMaxRequestBytes)
}

func (cfg *RuntimeConfig) EffectiveUploadMaxFiles() int {
	if cfg == nil {
		return DefaultUploadMaxFiles
	}
	return clampPositiveLimit(cfg.UploadMaxFiles, DefaultUploadMaxFiles, MaxUploadMaxFiles)
}

func (cfg *RuntimeConfig) EffectiveUploadMaxFileBytes() int {
	if cfg == nil {
		return DefaultUploadMaxFileBytes
	}
	return clampPositiveLimit(cfg.UploadMaxFileBytes, DefaultUploadMaxFileBytes, MaxUploadMaxFileBytes)
}

func (cfg *RuntimeConfig) EffectiveUploadMaxTotalBytes() int {
	if cfg == nil {
		return DefaultUploadMaxTotalBytes
	}
	return clampPositiveLimit(cfg.UploadMaxTotalBytes, DefaultUploadMaxTotalBytes, MaxUploadMaxTotalBytes)
}

func (cfg *RuntimeConfig) EffectiveMemorySnapshotMaxBytes() int {
	if cfg == nil {
		return DefaultMemorySnapshotMaxBytes
	}
	return clampPositiveLimit(cfg.MemorySnapshotMaxBytes, DefaultMemorySnapshotMaxBytes, MaxMemorySnapshotMaxBytes)
}
