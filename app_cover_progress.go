package main

import (
	"sort"
	"sync"
	"time"

	"github.com/asam-masa/manga-update-manager/internal/application"
)

type CoverProgressDTO struct {
	OperationID         string `json:"operationID"`
	WorkID              int64  `json:"workID"`
	Kind                string `json:"kind"`
	Stage               string `json:"stage"`
	Timed               bool   `json:"timed"`
	ElapsedMilliseconds int64  `json:"elapsedMilliseconds"`
}

type coverProgressEntry struct {
	dto     CoverProgressDTO
	started time.Time
}

// This lock only protects in-memory observations, never App lifetime or I/O.
type coverProgressState struct {
	mu     sync.Mutex
	active map[string]*coverProgressEntry
	now    func() time.Time // fixed before use in tests; otherwise time.Now
}

func (p *coverProgressState) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *coverProgressState) begin(id string, workID int64, kind string) (application.CoverProgress, func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active == nil {
		p.active = make(map[string]*coverProgressEntry)
	}
	if _, exists := p.active[id]; exists {
		return nil, func() {}, false
	}
	entry := &coverProgressEntry{dto: CoverProgressDTO{OperationID: id, WorkID: workID, Kind: kind}}
	p.active[id] = entry
	notify := func(stage application.CoverProgressStage) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.active[id] != entry {
			return
		}
		entry.dto.Stage = string(stage)
		if !entry.dto.Timed && (kind != "select" || stage == application.CoverProgressGenerating) {
			entry.dto.Timed = true
			entry.started = p.clock()
		}
	}
	finish := func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.active[id] == entry {
			delete(p.active, id)
		}
	}
	return notify, finish, true
}

// LoadCoverProgress copies observations only. Absence is not a completion result.
// It intentionally does not acquire App.mu or consult a DB/image operation.
func (a *App) LoadCoverProgress() []CoverProgressDTO {
	p := &a.coverProgress
	p.mu.Lock()
	result := make([]CoverProgressDTO, 0, len(p.active))
	now := p.clock()
	for _, entry := range p.active {
		if entry.dto.Stage == "" {
			continue
		} // No admitted step has been observed.
		dto := entry.dto
		if dto.Timed {
			dto.ElapsedMilliseconds = now.Sub(entry.started).Milliseconds()
			if dto.ElapsedMilliseconds < 0 {
				dto.ElapsedMilliseconds = 0
			}
		}
		result = append(result, dto)
	}
	p.mu.Unlock()
	sort.Slice(result, func(i, j int) bool { return result[i].OperationID < result[j].OperationID })
	return result
}

func validCoverOperationID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (a *App) RemoveWorkCoverWithOperation(workID int64, operationID string) (RemoveWorkCoverResult, error) {
	if !validCoverOperationID(operationID) {
		return RemoveWorkCoverResult{}, invalidCoverOperationID()
	}
	progress, finish, ok := a.coverProgress.begin(operationID, workID, "remove")
	if !ok {
		return RemoveWorkCoverResult{}, coverAPIError(application.ErrCoverBusy)
	}
	defer finish()
	return a.removeWorkCover(workID, progress)
}
