// Package restore validates recovery records independently of filesystem I/O.
package restore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/asam-masa/manga-update-manager/internal/backup"
)

const (
	ActiveDirectory    = "restore-active"
	PlanName           = "plan.json"
	CompletionName     = "complete.json"
	MaxPlanBytes       = 8 << 20
	MaxCompletionBytes = 4 << 10
)

var (
	ErrRecordsUncertain   = errors.New("restore record observations are incomplete")
	ErrInvalidRecord      = errors.New("invalid restore recovery record")
	ErrRecordPairMismatch = errors.New("restore plan and completion do not match")
)

type ObservationState uint8

const (
	Unknown ObservationState = iota
	Absent
	Present
)

// Observation describes acquisition, not whether the acquired bytes are valid.
type Observation struct {
	State ObservationState
	Bytes []byte
	Err   error
}

type Decision uint8

const (
	Stop Decision = iota
	NoRecords
	IncompleteCandidate
	CompleteCandidate
)

type FileIdentity struct {
	VolumeSerial, FileID string
}

type StagedFile struct {
	StagedID string
	Identity FileIdentity
}

type StagedImage struct {
	ImageID string
	StagedFile
}

type Plan struct {
	RestoreID, WorkspaceID string
	OldDatabase            FileIdentity
	OldCovers              *FileIdentity
	NewDatabase            StagedFile
	NewImages              []StagedImage
}

// Classify consumes observations, including the presence of an unpublished
// candidate. It does not authorize startup or any filesystem operation.
func Classify(plan, completion Observation, unpublished ObservationState) (Decision, *Plan, error) {
	if unpublished != Absent || !usable(plan) || !usable(completion) {
		return Stop, nil, errors.Join(ErrRecordsUncertain, plan.Err, completion.Err)
	}
	if plan.State == Absent {
		if completion.State != Absent {
			return Stop, nil, ErrRecordPairMismatch
		}
		return NoRecords, nil, nil
	}
	decoded, err := decodePlan(plan.Bytes)
	if err != nil {
		return Stop, nil, fmt.Errorf("restore plan: %w", err)
	}
	if completion.State == Absent {
		return IncompleteCandidate, decoded, nil
	}
	fields, err := object(completion.Bytes, MaxCompletionBytes, "version", "restoreId", "planSha256")
	if err != nil || string(fields["version"]) != "1" {
		return Stop, nil, fmt.Errorf("restore completion: %w", ErrInvalidRecord)
	}
	restoreID, idOK := lowerHex(fields["restoreId"], 32)
	digest, digestOK := lowerHex(fields["planSha256"], 64)
	if !idOK || !digestOK {
		return Stop, nil, fmt.Errorf("restore completion: %w", ErrInvalidRecord)
	}
	hash := sha256.Sum256(plan.Bytes)
	if restoreID != decoded.RestoreID || digest != hex.EncodeToString(hash[:]) {
		return Stop, nil, ErrRecordPairMismatch
	}
	return CompleteCandidate, decoded, nil
}

func usable(o Observation) bool {
	return o.Err == nil && (o.State == Present || (o.State == Absent && len(o.Bytes) == 0))
}

func decodePlan(data []byte) (*Plan, error) {
	fields, err := object(data, MaxPlanBytes, "version", "restoreId", "workspaceId", "oldDatabase", "oldCovers", "newDatabase", "newImages")
	if err != nil || string(fields["version"]) != "1" {
		return nil, ErrInvalidRecord
	}
	restoreID, idOK := lowerHex(fields["restoreId"], 32)
	workspaceID, workspaceOK := lowerHex(fields["workspaceId"], 32)
	oldDB, oldOK := identity(fields["oldDatabase"])
	newFields, err := object(fields["newDatabase"], MaxPlanBytes, "stagedId", "identity")
	if !idOK || !workspaceOK || !oldOK || err != nil {
		return nil, ErrInvalidRecord
	}
	newDB, ok := stagedFile(newFields)
	if !ok || newDB.Identity == oldDB {
		return nil, ErrInvalidRecord
	}
	p := &Plan{RestoreID: restoreID, WorkspaceID: workspaceID, OldDatabase: oldDB, NewDatabase: newDB}
	seenIdentity := map[FileIdentity]bool{oldDB: true, newDB.Identity: true}
	if string(fields["oldCovers"]) != "null" {
		covers, ok := identity(fields["oldCovers"])
		if !ok || seenIdentity[covers] {
			return nil, ErrInvalidRecord
		}
		seenIdentity[covers] = true
		p.OldCovers = &covers
	}
	seenStaged := map[string]bool{newDB.StagedID: true}
	seenImage := make(map[string]bool)
	d := json.NewDecoder(bytes.NewReader(fields["newImages"]))
	if token, err := d.Token(); err != nil || token != json.Delim('[') {
		return nil, ErrInvalidRecord
	}
	p.NewImages = []StagedImage{}
	for d.More() {
		var raw json.RawMessage
		if len(p.NewImages) == backup.MaxImages || d.Decode(&raw) != nil {
			return nil, ErrInvalidRecord
		}
		fields, err := object(raw, MaxPlanBytes, "imageId", "stagedId", "identity")
		if err != nil {
			return nil, err
		}
		imageID, imageOK := lowerHex(fields["imageId"], 32)
		file, fileOK := stagedFile(fields)
		if !imageOK || !fileOK || seenImage[imageID] || seenStaged[file.StagedID] || seenIdentity[file.Identity] {
			return nil, ErrInvalidRecord
		}
		seenImage[imageID], seenStaged[file.StagedID], seenIdentity[file.Identity] = true, true, true
		p.NewImages = append(p.NewImages, StagedImage{ImageID: imageID, StagedFile: file})
	}
	if token, err := d.Token(); err != nil || token != json.Delim(']') {
		return nil, ErrInvalidRecord
	}
	return p, nil
}

func stagedFile(fields map[string]json.RawMessage) (StagedFile, bool) {
	id, idOK := lowerHex(fields["stagedId"], 32)
	physical, physicalOK := identity(fields["identity"])
	return StagedFile{StagedID: id, Identity: physical}, idOK && physicalOK
}

func identity(raw []byte) (FileIdentity, bool) {
	fields, err := object(raw, MaxPlanBytes, "volumeSerial", "fileId")
	if err != nil {
		return FileIdentity{}, false
	}
	volume, volumeOK := lowerHex(fields["volumeSerial"], 16)
	file, fileOK := lowerHex(fields["fileId"], 32)
	return FileIdentity{VolumeSerial: volume, FileID: file}, volumeOK && fileOK
}

func lowerHex(raw []byte, length int) (string, bool) {
	var value string
	if json.Unmarshal(raw, &value) != nil || len(value) != length {
		return "", false
	}
	for i := range len(value) {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", false
		}
	}
	return value, true
}

// object consumes exactly one object with the specified keys. Token compares
// decoded keys, so alternate escaped spellings cannot hide duplicates.
func object(data []byte, maxBytes int, keys ...string) (map[string]json.RawMessage, error) {
	if len(data) == 0 || len(data) > maxBytes || !utf8.Valid(data) {
		return nil, ErrInvalidRecord
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return nil, ErrInvalidRecord
	}
	fields := make(map[string]json.RawMessage, len(keys))
	for _, key := range keys {
		fields[key] = nil
	}
	for d.More() {
		token, err := d.Token()
		key, isString := token.(string)
		prior, allowed := fields[key]
		if err != nil || !isString || !allowed || prior != nil {
			return nil, ErrInvalidRecord
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, ErrInvalidRecord
		}
		fields[key] = raw
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalidRecord
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrInvalidRecord
	}
	for _, field := range fields {
		if field == nil {
			return nil, ErrInvalidRecord
		}
	}
	return fields, nil
}
