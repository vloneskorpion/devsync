package shared

import "sync"

type SharedData struct {
	Mu       sync.Mutex
	Snapshot map[string]FileInfo
}
