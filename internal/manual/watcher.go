package manual

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch monitors dir using both fsnotify and a periodic hash check (crucial for
// Kubernetes ConfigMap symlink swaps on ..data) and sends reloaded inventories on ch.
func Watch(ctx context.Context, dir string, lastHash string, pollInterval time.Duration, log *slog.Logger) <-chan *Inventory {
	out := make(chan *Inventory, 4)
	if dir == "" {
		close(out)
		return out
	}
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}

	go func() {
		defer close(out)

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Warn("fsnotify unavailable, falling back to polling", "err", err)
		} else {
			defer watcher.Close()
			addWatchDirs(watcher, dir, log)
		}

		var debounceTimer *time.Timer
		var debounceC <-chan time.Time
		pollTicker := time.NewTicker(pollInterval)
		defer pollTicker.Stop()

		reloadIfChanged := func() {
			inv, err := LoadDir(dir)
			if err != nil {
				log.Error("failed reloading manual YAML", "dir", dir, "err", err)
				return
			}
			if inv.Hash == lastHash {
				return
			}
			lastHash = inv.Hash
			if watcher != nil {
				addWatchDirs(watcher, dir, log)
			}
			select {
			case out <- inv:
			case <-ctx.Done():
			}
		}

		var watchEvents <-chan fsnotify.Event
		var watchErrors <-chan error
		if watcher != nil {
			watchEvents = watcher.Events
			watchErrors = watcher.Errors
		}

		for {
			select {
			case <-ctx.Done():
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				return
			case ev, ok := <-watchEvents:
				if !ok {
					watchEvents = nil
					continue
				}
				base := filepath.Base(ev.Name)
				// Watch ..data symlink swaps in Kubernetes as well as normal YAML edits.
				if strings.HasPrefix(base, ".") && base != "..data" {
					continue
				}
				if debounceTimer == nil {
					debounceTimer = time.NewTimer(300 * time.Millisecond)
					debounceC = debounceTimer.C
				} else {
					debounceTimer.Reset(300 * time.Millisecond)
				}
			case err, ok := <-watchErrors:
				if !ok {
					watchErrors = nil
					continue
				}
				log.Warn("manual YAML watcher error", "err", err)
			case <-debounceC:
				debounceC = nil
				debounceTimer = nil
				reloadIfChanged()
			case <-pollTicker.C:
				if _, err := os.Stat(dir); err == nil {
					reloadIfChanged()
				}
			}
		}
	}()

	return out
}

func addWatchDirs(w *fsnotify.Watcher, root string, log *slog.Logger) {
	if _, err := os.Stat(root); err != nil {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != root && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if err := w.Add(path); err != nil {
			log.Debug("failed watching dir", "path", path, "err", err)
		}
		return nil
	})
}
