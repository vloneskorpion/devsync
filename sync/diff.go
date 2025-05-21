package sync

import (
	"devsync/shared"
)

type DiffType int

const (
	DiffMissingRemote DiffType = iota
	// DiffMissingLocal
	DiffDifferent
)

type DiffEntry struct {
	Type   DiffType
	Path   string
	Local  shared.FileInfo
	Remote shared.FileInfo
}

func CheckForSnapshotDifferences(local, remote *shared.SharedData) []DiffEntry {
	diffs := make([]DiffEntry, 0)

	local.Mu.Lock()
	remote.Mu.Lock()
	defer local.Mu.Unlock()
	defer remote.Mu.Unlock()

	for path, localInfo := range local.Snapshot {
		remoteInfo, ok := remote.Snapshot[path]
		if !ok {
			diffs = append(diffs, DiffEntry{
				Path:   path,
				Type:   DiffMissingRemote,
				Local:  localInfo,
				Remote: shared.FileInfo{},
			})
			continue
		}
		if localInfo.Size != remoteInfo.Size || localInfo.ModTime != remoteInfo.ModTime {
			diffs = append(diffs, DiffEntry{
				Path:   path,
				Type:   DiffDifferent,
				Local:  localInfo,
				Remote: remoteInfo,
			})
		}
	}

	// Check files existing remotely but missing locally - future feature
	// for path, remoteInfo := range remote.Snapshot {
	// 	if _, ok := local.Snapshot[path]; !ok {
	// 		diffs = append(diffs, DiffEntry{
	// 			Path:   path,
	// 			Type:   DiffMissingLocal,
	// 			Local:  shared.FileInfo{},
	// 			Remote: remoteInfo,
	// 		})
	// 	}
	// }

	return diffs
}
