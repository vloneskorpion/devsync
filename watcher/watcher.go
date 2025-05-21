package watcher

import (
	"devsync/shared"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

func isOneOfMany(event fsnotify.Event, ops ...fsnotify.Op) bool {
	for _, op := range ops {
		if event.Op.Has(op) {
			return true
		}
	}
	return false
}

func watchDirFunc(watcher *fsnotify.Watcher, localSnapshot *shared.SharedData, localPath string) fs.WalkDirFunc {
	return func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return watcher.Add(path)
		} else {
			fileInfo, err := d.Info()
			if err != nil {
				return err
			}
			relPath, _ := filepath.Rel(localPath, path)
			localSnapshot.Mu.Lock()
			localSnapshot.Snapshot[relPath] = shared.FileInfo{
				ModTime: fileInfo.ModTime().Unix(),
				Size:    fileInfo.Size(),
			}
			localSnapshot.Mu.Unlock()
		}
		return nil
	}
}

type Watcher struct {
	watcher       *fsnotify.Watcher
	localSnapshot *shared.SharedData
	initSync      chan struct{}
}

func NewWatcher(path string, localSnapshot *shared.SharedData, initSync chan struct{}) *Watcher {
	return &Watcher{
		watcher:       nil,
		localSnapshot: localSnapshot,
		initSync:      initSync,
	}
}

func (w *Watcher) Init() error {
	var err error
	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	return nil
}

func (w *Watcher) Watch(localPath string, eventChan chan fsnotify.Event) {
	var err error
	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		slog.Error("Failed to create watcher", "error", err)
		return
	}
	defer w.watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-w.watcher.Events:
				if !ok {
					return
				}

				if isOneOfMany(event, fsnotify.Rename, fsnotify.Remove) {
					_, err := os.Stat(event.Name)
					if os.IsNotExist(err) {
						w.watcher.Remove(event.Name)
						relPath, _ := filepath.Rel(localPath, event.Name)
						w.localSnapshot.Mu.Lock()
						delete(w.localSnapshot.Snapshot, relPath)
						w.localSnapshot.Mu.Unlock()
					}
				}

				if isOneOfMany(event, fsnotify.Write, fsnotify.Create, fsnotify.Chmod) {
					fileInfo, err := os.Stat(event.Name)
					if err != nil {
						slog.Error("Failed to stat file", "error", err)
						continue
					}
					if !fileInfo.IsDir() {
						relPath, _ := filepath.Rel(localPath, event.Name)
						w.localSnapshot.Mu.Lock()
						w.localSnapshot.Snapshot[relPath] = shared.FileInfo{
							ModTime: fileInfo.ModTime().Unix(),
							Size:    fileInfo.Size(),
						}
						w.localSnapshot.Mu.Unlock()
					} else {
						if event.Has(fsnotify.Create) {
							w.watcher.Add(event.Name)
							filepath.WalkDir(event.Name, watchDirFunc(w.watcher, w.localSnapshot, localPath))
						}
					}
					eventChan <- event
				}

			case err, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				slog.Error("Watcher error:", "error", err)
			}
		}
	}()

	err = filepath.WalkDir(localPath, watchDirFunc(w.watcher, w.localSnapshot, localPath))
	if err != nil {
		slog.Error("Failed to walk directory", "error", err, "localPath", localPath)
	}

	w.initSync <- struct{}{}

	// block forever
	<-make(chan struct{})
}

func (w *Watcher) Close() {
	w.watcher.Close()
}
