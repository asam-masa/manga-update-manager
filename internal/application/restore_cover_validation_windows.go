package application

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"io"

	"github.com/asam-masa/manga-update-manager/internal/backup"
	"github.com/asam-masa/manga-update-manager/internal/thumbnail"
)

var (
	errRestoreCoverReference = errors.New("restore database and manifest cover references differ")
	errRestoreCoverContent   = errors.New("restore cover content is invalid")
)

// checkPreparedRestoreWithImages checks the prepared snapshot before placement.
func checkPreparedRestoreWithImages(ctx context.Context, p *PreparedRestore) (restoreSQLResult, error) {
	return checkPreparedRestore(ctx, p, true)
}

func (s *restoreSQLSession) checkCoverReferences(ctx context.Context, r *restoreSQLResult, p *PreparedRestore) {
	if r.OperationErr != nil || r.ReleaseErr != nil || s.failure() != nil || ctx.Err() != nil {
		return
	}
	included := make(map[string]backup.Image, len(p.manifest.Images))
	omitted := make(map[string]string, len(p.manifest.OmittedImages))
	for _, image := range p.manifest.Images {
		included[image.ID] = image
	}
	for _, image := range p.manifest.OmittedImages {
		omitted[image.ID] = image.Path
	}
	count := len(included) + len(omitted)
	// Scope the inventory to referenced paths, including missing managed
	// records. A shared path contributes one row.
	const query = `SELECT refs.thumbnail_path,c.id,c.relative_path,c.state,c.byte_size,c.sha256
FROM (SELECT DISTINCT thumbnail_path FROM main.works WHERE thumbnail_path<>'' LIMIT ?) refs
LEFT JOIN main.managed_covers c ON refs.thumbnail_path=c.relative_path`
	s.compareRows(ctx, r, query, []driver.NamedValue{{Ordinal: 1, Value: int64(count + 1)}}, count, 6, errRestoreCoverReference,
		func(_ int, row []driver.Value) bool {
			id, ok := row[1].(string)
			path, pathOK := row[2].(string)
			if !ok || !pathOK || row[0] != path || row[3] != "ready" {
				return false
			}
			if image, ok := included[id]; ok {
				digest, digestOK := row[5].([]byte)
				if path != image.Path || row[4] != image.Size || !digestOK || len(digest) != sha256.Size || hex.EncodeToString(digest) != image.SHA256 {
					return false
				}
				delete(included, id)
				return true
			}
			expectedPath, ok := omitted[id]
			if !ok || path != expectedPath {
				return false
			}
			delete(omitted, id)
			return true
		})
	if r.OperationErr == nil && r.ReleaseErr == nil && s.failure() == nil && ctx.Err() == nil {
		r.OperationErr = validatePreparedCoverContents(ctx, p)
	}
}

func validatePreparedCoverContents(ctx context.Context, p *PreparedRestore) error {
	files := make(map[string]PreparedRestoreFile, len(p.manifest.Images))
	for _, file := range p.files {
		if file.ImageID != "" {
			files[file.ImageID] = file
		}
	}
	for _, image := range p.manifest.Images {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, ok := files[image.ID]
		if !ok || file.Reader == nil || file.Metadata.Path != image.Path ||
			file.Metadata.Size != image.Size || file.Metadata.SHA256 != image.SHA256 ||
			image.Size <= 0 || image.Size > backup.MaxImageBytes {
			return errRestoreCoverContent
		}
		reader := restoreReadbackReaderAt{ctx: ctx, file: file.Reader}
		data, err := io.ReadAll(io.NewSectionReader(reader, 0, image.Size+1))
		if err != nil {
			return errors.Join(errRestoreCoverContent, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if int64(len(data)) != image.Size || hex.EncodeToString(digest[:]) != image.SHA256 {
			return errRestoreCoverContent
		}
		if err := thumbnail.ValidateCover(data); err != nil {
			return errors.Join(errRestoreCoverContent, err)
		}
	}
	return ctx.Err()
}
