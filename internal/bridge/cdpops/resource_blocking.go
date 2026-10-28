package cdpops

import (
	"context"
	"strings"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

var ImageBlockPatterns = []string{
	"*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp", "*.svg", "*.ico",
}

var MediaBlockPatterns = append(ImageBlockPatterns,
	"*.mp4", "*.webm", "*.ogg", "*.mp3", "*.wav", "*.flac", "*.aac",
)

// SetResourceBlocking uses Network.setBlockedURLs to block resources by URL pattern.
func SetResourceBlocking(ctx context.Context, patterns []string) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if len(patterns) == 0 {
			return network.SetBlockedURLs().Do(ctx)
		}
		blockPatterns := make([]*network.BlockPattern, 0, len(patterns))
		for _, pattern := range patterns {
			if pattern == "" {
				continue
			}
			for _, p := range absoluteURLPatterns(pattern) {
				blockPatterns = append(blockPatterns, &network.BlockPattern{URLPattern: p, Block: true})
			}
		}
		return network.SetBlockedURLs().WithURLPatterns(blockPatterns).Do(ctx)
	}))
}

// absoluteURLPatterns turns the globs above into the absolute URLPattern
// strings urlPatterns requires; Chrome ignores a relative one like "*.jpg".
//
//	"*.jpg"              -> "*://*:*/*.jpg"
//	"*doubleclick.net/*" -> "*://*doubleclick.net:*/*"
//	"*/collect?*"        -> "/collect?…" at the root or under any directory
//	"*analytics.gif*"    -> "*://*:*/*analytics.gif*"
func absoluteURLPatterns(glob string) []string {
	switch {
	case strings.Contains(glob, "://"):
		return []string{glob}
	case strings.HasPrefix(glob, "*."):
		return []string{"*://*:*/" + glob}
	case strings.HasPrefix(glob, "*/"):
		rest := glob[2:]
		return []string{"*://*:*/" + rest, "*://*:*/*/" + rest}
	}
	host, path, found := strings.Cut(glob, "/")
	if !found || !strings.Contains(host, ".") || strings.ContainsAny(host, "?") {
		return []string{"*://*:*/" + glob}
	}
	if path == "" {
		path = "*"
	}
	return []string{"*://" + host + ":*/" + path}
}
