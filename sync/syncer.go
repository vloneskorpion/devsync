package sync

import (
	"devsync/shared"
	"devsync/watcher"
	"fmt"
	"sync"
	"time"

	"log/slog"

	"github.com/fsnotify/fsnotify"
)

type Config struct {
	User       string
	Ip         string
	Password   string
	Port       string
	PrivateKey string
	KnownHosts string
}

type Syncer struct {
	basePath       string
	remoteBasePath string
	localSnapshot  shared.SharedData
	remoteSnapshot shared.SharedData
	watcher        *watcher.Watcher
	uploader       *Uploader
	event          chan fsnotify.Event
	syncTrigger    chan struct{}
	initSync       chan struct{}
	config         Config
}

func NewSyncer(basePath string, remoteBasePath string, config Config) *Syncer {
	return &Syncer{
		basePath:       basePath,
		remoteBasePath: remoteBasePath,
		localSnapshot:  shared.SharedData{Mu: sync.Mutex{}, Snapshot: make(map[string]shared.FileInfo)},
		remoteSnapshot: shared.SharedData{Mu: sync.Mutex{}, Snapshot: make(map[string]shared.FileInfo)},
		watcher:        nil,
		uploader:       nil,
		event:          make(chan fsnotify.Event, 10000),
		syncTrigger:    make(chan struct{}, 1),
		initSync:       make(chan struct{}),
		config:         config,
	}
}

func (s *Syncer) Init() error {
	var err error
	s.watcher = watcher.NewWatcher(s.basePath, &s.localSnapshot, s.initSync)
	err = s.watcher.Init()
	if err != nil {
		return err
	}

	s.uploader, err = NewUploader(s.config)
	if err != nil {
		return err
	}

	err = s.uploader.Init()
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}

	return nil
}

func (s *Syncer) eventCollector() {
	const (
		debounceDelay = 500 * time.Millisecond
		maxDelay      = 3 * time.Second
	)

	dedup := make(map[string]bool)
	timer := time.NewTimer(time.Hour)
	timer.Stop()

	lastSync := time.Now()

	for {
		select {
		case evt := <-s.event:
			dedup[evt.Name] = true
			//Trigger sync if too many events are queued
			if len(dedup) > 1000 {
				s.triggerSync()
				dedup = make(map[string]bool)
				lastSync = time.Now()
				timer.Stop()
			}
			timer.Reset(debounceDelay)

			// Throttle: ensure max delay is not exceeded
			if time.Since(lastSync) > maxDelay {
				s.triggerSync()
				dedup = make(map[string]bool)
				lastSync = time.Now()
				timer.Stop()
			}

		case <-timer.C:
			if len(dedup) > 0 {
				s.triggerSync()
				dedup = make(map[string]bool)
				lastSync = time.Now()
			}
		}
	}
}

func (s *Syncer) triggerSync() {
	select {
	case s.syncTrigger <- struct{}{}:
	default: // drop if already queued
	}
}

func (s *Syncer) Run() error {
	err := s.Init()
	if err != nil {
		return err
	}

	go func() {
		s.watcher.Watch(s.basePath, s.event)
	}()

	go s.eventCollector()

	<-s.initSync
	s.triggerSync()

	for range s.syncTrigger {
		s.remoteSnapshot.Mu.Lock()
		s.remoteSnapshot.Snapshot, err = s.uploader.GetRemoteSnapshot(s.GetRemotePath())
		s.remoteSnapshot.Mu.Unlock()
		if err != nil {
			slog.Warn("Failed to get remote snapshot", "warning", err)
		}

		diffs := CheckForSnapshotDifferences(&s.localSnapshot, &s.remoteSnapshot)

		// 	// case DiffMissingLocal: // for now skip -d option for remote
		// 	// 	s.uploader.Delete(s.GetRemotePath() + diff.Path)
		// 	// 	fmt.Println("Missing on local: ", diff.Path)

		type UploadJob struct {
			localBasePath string
			localFilePath string
			remoteFolder  string
		}

		jobs := make(chan UploadJob, 50)
		wg := sync.WaitGroup{}
		workerCount := 50 // Tune this based on CPU/network

		for i := 0; i < workerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					_ = s.uploader.Sync(job.localBasePath, job.localFilePath, job.remoteFolder)
				}
			}()
		}

		for _, diff := range diffs {
			if diff.Type == DiffMissingRemote || diff.Type == DiffDifferent {
				jobs <- UploadJob{
					localBasePath: s.GetBasePath(),
					localFilePath: diff.Path,
					remoteFolder:  s.GetRemotePath(),
				}
			}
		}
		close(jobs)
		wg.Wait()
	}

	select {}
	return nil
}

func (s *Syncer) Close() {
	s.watcher.Close()
	s.uploader.Close()
}

func (s *Syncer) PrintLocalSnapshot() {
	s.localSnapshot.Mu.Lock()
	defer s.localSnapshot.Mu.Unlock()

	for path, info := range s.localSnapshot.Snapshot {
		fmt.Printf("%s: %d bytes, modified %d\n", path, info.Size, info.ModTime)
	}
}

func (s *Syncer) PrintRemoteSnapshot() {
	s.remoteSnapshot.Mu.Lock()
	defer s.remoteSnapshot.Mu.Unlock()

	for path, info := range s.remoteSnapshot.Snapshot {
		fmt.Printf("%s: %d bytes, modified %d\n", path, info.Size, info.ModTime)
	}
}

func (s *Syncer) GetLocalSnapshot() (map[string]shared.FileInfo, error) {
	s.localSnapshot.Mu.Lock()
	defer s.localSnapshot.Mu.Unlock()
	return s.localSnapshot.Snapshot, nil
}

func (s *Syncer) GetRemotePath() string {
	return s.remoteBasePath
}

func (s *Syncer) GetBasePath() string {
	return s.basePath
}
