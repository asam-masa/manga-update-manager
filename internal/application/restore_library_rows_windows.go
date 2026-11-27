package application

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"

	"github.com/asam-masa/manga-update-manager/internal/manga"
	"github.com/asam-masa/manga-update-manager/internal/storage"
)

var (
	errRestoreWorkRow = errors.New("invalid stored work in restore database")
	errRestoreTagRow  = errors.New("invalid stored tag in restore database")
	errRestoreLinkRow = errors.New("invalid stored work-tag link in restore database")
)

func (s *restoreSQLSession) checkLibraryRows(ctx context.Context, r *restoreSQLResult) {
	const allRows = -1
	var previousWork int64
	s.compareRows(ctx, r, storage.LibraryWorksQuery, nil, allRows, 10, errRestoreWorkRow, func(_ int, row []driver.Value) bool {
		work, err := storage.DecodeLibraryWork(func(dest ...any) error {
			return scanRestoreWork(row, dest)
		})
		if err != nil || work.ID <= previousWork {
			return false
		}
		previousWork = work.ID
		return true
	})

	var previousTag int64
	s.compareRows(ctx, r, "SELECT id,name FROM main.tags ORDER BY id", nil, allRows, 2, errRestoreTagRow, func(_ int, row []driver.Value) bool {
		id, idOK := row[0].(int64)
		name, nameOK := row[1].(string)
		if !idOK || !nameOK || id <= previousTag || manga.TagName(name).Validate() != nil {
			return false
		}
		previousTag = id
		return true
	})

	var previous storage.WorkTagLink
	const links = `SELECT l.work_id,l.tag_id,w.id,t.id FROM main.work_tags l
LEFT JOIN main.works w ON w.id=l.work_id LEFT JOIN main.tags t ON t.id=l.tag_id
ORDER BY l.work_id,l.tag_id`
	s.compareRows(ctx, r, links, nil, allRows, 4, errRestoreLinkRow, func(_ int, row []driver.Value) bool {
		work, workOK := row[0].(int64)
		tag, tagOK := row[1].(int64)
		if !workOK || !tagOK || work <= 0 || tag <= 0 || row[2] != work || row[3] != tag ||
			work < previous.WorkID || (work == previous.WorkID && tag <= previous.TagID) {
			return false
		}
		previous = storage.WorkTagLink{WorkID: work, TagID: tag}
		return true
	})
}

// The approved STRICT schema supplies only integer, text, and nullable text
// for this work projection. Reject any other driver representation.
func scanRestoreWork(row []driver.Value, dest []any) error {
	if len(row) != len(dest) {
		return errRestoreWorkRow
	}
	for i, target := range dest {
		switch target := target.(type) {
		case *int64:
			value, ok := row[i].(int64)
			if !ok {
				return errRestoreWorkRow
			}
			*target = value
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return errRestoreWorkRow
			}
			*target = value
		case *sql.NullString:
			if row[i] != nil {
				if _, ok := row[i].(string); !ok {
					return errRestoreWorkRow
				}
			}
			if err := target.Scan(row[i]); err != nil {
				return err
			}
		default:
			return errRestoreWorkRow
		}
	}
	return nil
}
