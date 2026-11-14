package main

import (
	"errors"
	"slices"
	"strings"

	"github.com/asam-masa/manga-update-manager/internal/application"
	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

const backupDetailPageSize = 50

var (
	errBackupDetailRequest     = errors.New("invalid backup detail request")
	errBackupDetailStale       = errors.New("backup detail execution expired")
	errBackupDetailPending     = errors.New("backup result not yet retained")
	errBackupDetailUnobserved  = errors.New("backup detail not observed")
	errBackupDetailUnavailable = errors.New("backup work name details unavailable")
	errBackupDetailImage       = errors.New("omitted backup image not found")
)

// Internal views only. Raw causes are immutable diagnostics, not public DTOs.
// Counts are nil when unknown; a pointer to zero is an observed empty result.
type backupResultOverview struct {
	backupDatabaseRunResult
	Save                   application.BackupSaveResult
	OutputCreationError    error
	OutputReleaseConfirmed bool
	OmittedImageCount      *int
	WorkNamesState         backupWorkNamesState
	WorkCount              *int
	DetailError            error
	Err                    error
}

type backupRunOverview struct {
	execution   application.BackupExecutionSnapshot
	progress    backupRunProgress
	destination string
	result      *backupResultOverview // Nil until cleanup/result storage finishes.
}

func (s *backupRunState) overview() backupRunOverview {
	header, stored := s.inspect()
	view := backupRunOverview{execution: header.execution, progress: header.progress, destination: header.destination}
	if stored == nil {
		return view
	}
	r := stored.save // No Manifest/Covers/Works cloning for status polling.
	result := &backupResultOverview{backupDatabaseRunResult: r.backupDatabaseRunResult, Save: r.Save,
		OutputCreationError: r.OutputCreationError, OutputReleaseConfirmed: r.OutputReleaseConfirmed,
		WorkNamesState: r.Omissions.WorkNamesState, DetailError: r.Omissions.DetailError, Err: stored.err}
	result.Warnings = slices.Clone(result.Warnings)
	if result.Save.Cleanup != nil {
		cleanup := *result.Save.Cleanup
		result.Save.Cleanup = &cleanup
	}
	if r.Omissions.Known {
		count := len(r.Omissions.Images)
		result.OmittedImageCount = &count
	}
	if r.Omissions.WorkNamesState == backupWorkNamesAvailable {
		count := len(r.Omissions.Works)
		result.WorkCount = &count
	}
	view.result = result
	return view
}

type backupPageInfo struct {
	ID                           application.BackupExecutionID
	Page, PageSize, Pages, Total int
}

type backupImagePage struct {
	backupPageInfo
	Items []backup.OmittedImage
}

type backupWorkPage struct {
	backupPageInfo
	ImageID string
	Items   []storage.CopyCoverWork
}

// Page validation precedes offset multiplication, including for huge integers.
// An observed empty list has one empty page, numbered 1.
func backupPage(id application.BackupExecutionID, page, total int) (backupPageInfo, int, int, error) {
	pages := 1
	if total > 0 {
		pages = 1 + (total-1)/backupDetailPageSize
	}
	if page < 1 || page > pages {
		return backupPageInfo{}, 0, 0, errBackupDetailRequest
	}
	first := (page - 1) * backupDetailPageSize
	last := first + min(backupDetailPageSize, total-first)
	return backupPageInfo{ID: id, Page: page, PageSize: backupDetailPageSize, Pages: pages, Total: total}, first, last, nil
}

// The captured result remains immutable if a new run starts after this read.
// Every response carries its original ID; callers must not attach it to a new ID.
func (s *backupRunState) detailSource(id application.BackupExecutionID) (*backupRunResult, error) {
	if id == 0 {
		return nil, errBackupDetailRequest
	}
	header, stored := s.inspect()
	if header.execution.ID != id {
		return nil, errBackupDetailStale
	}
	if stored == nil {
		return nil, errBackupDetailPending
	}
	if !stored.save.Omissions.Known {
		return nil, errBackupDetailUnobserved
	}
	return stored, nil
}

func (s *backupRunState) imagePage(id application.BackupExecutionID, page int) (backupImagePage, error) {
	if page < 1 {
		return backupImagePage{}, errBackupDetailRequest
	}
	stored, err := s.detailSource(id)
	if err != nil {
		return backupImagePage{}, err
	}
	images := stored.save.Omissions.Images
	info, first, last, err := backupPage(id, page, len(images))
	if err != nil {
		return backupImagePage{}, err
	}
	return backupImagePage{backupPageInfo: info, Items: append([]backup.OmittedImage{}, images[first:last]...)}, nil
}

func (s *backupRunState) workPage(id application.BackupExecutionID, imageID string, page int) (backupWorkPage, error) {
	if page < 1 || len(imageID) != 32 || strings.Trim(imageID, "0123456789abcdef") != "" {
		return backupWorkPage{}, errBackupDetailRequest
	}
	stored, err := s.detailSource(id)
	if err != nil {
		return backupWorkPage{}, err
	}
	omissions := stored.save.Omissions
	if !slices.ContainsFunc(omissions.Images, func(image backup.OmittedImage) bool { return image.ID == imageID }) {
		return backupWorkPage{}, errBackupDetailImage
	}
	if omissions.WorkNamesState == backupWorkNamesUnobserved {
		return backupWorkPage{}, errBackupDetailUnobserved
	}
	if omissions.WorkNamesState != backupWorkNamesAvailable {
		return backupWorkPage{}, errBackupDetailUnavailable
	}
	total := 0
	for _, work := range omissions.Works {
		if work.CoverID == imageID {
			total++
		}
	}
	info, first, last, err := backupPage(id, page, total)
	if err != nil {
		return backupWorkPage{}, err
	}
	items := make([]storage.CopyCoverWork, 0, last-first)
	index := 0
	for _, work := range omissions.Works {
		if work.CoverID == imageID {
			if index >= first && index < last {
				items = append(items, work)
			}
			index++
		}
	}
	return backupWorkPage{backupPageInfo: info, ImageID: imageID, Items: items}, nil
}
