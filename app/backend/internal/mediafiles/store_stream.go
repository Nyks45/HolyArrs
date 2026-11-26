package mediafiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
)

// LocalStreamFile describes a local video file already in the Library that
// matches a movie or episode selected for playback.
type LocalStreamFile struct {
	FileID       int64  `json:"file_id"`
	TitleID      int64  `json:"title_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Year         int    `json:"year,omitempty"`
	Season       int    `json:"season,omitempty"`
	Episode      int    `json:"episode,omitempty"`
	RelPath      string `json:"rel_path"`
	RootPath     string `json:"root_path"`
	FullPath     string `json:"-"`
	Size         int64  `json:"size"`
	Quality      string `json:"quality,omitempty"`
	Codec        string `json:"codec,omitempty"`
	Container    string `json:"container,omitempty"`
	VersionLabel string `json:"version_label,omitempty"`
}

// ErrLocalFileUnavailable is returned when a local Library file cannot be resolved safely on disk.
var ErrLocalFileUnavailable = errors.New("local library file is unavailable")

// FirstLibraryFolderPath returns the root path of the primary Library folder.
func FirstLibraryFolderPath(ctx context.Context, db database.Querier) (string, error) {
	if db == nil {
		return "", sql.ErrNoRows
	}
	var path string
	err := db.QueryRowContext(ctx, `SELECT path FROM library_folders WHERE stand_in = 0 ORDER BY id LIMIT 1`).Scan(&path)
	if err == nil && strings.TrimSpace(path) != "" {
		return filepath.Clean(path), nil
	}
	err = db.QueryRowContext(ctx, `SELECT path FROM library_folders ORDER BY id LIMIT 1`).Scan(&path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}

// FindLocalVideoFiles lists present main video files in the Library matching
// a movie or show episode by TMDB id.
func FindLocalVideoFiles(ctx context.Context, db database.Lister, kind, tmdbID string, season, episode int) ([]LocalStreamFile, error) {
	if db == nil || strings.TrimSpace(tmdbID) == "" {
		return nil, nil
	}
	tmdbID = strings.TrimSpace(tmdbID)
	kind = strings.ToLower(strings.TrimSpace(kind))

	var rows *sql.Rows
	var err error
	if kind == "show" && season > 0 && episode > 0 {
		rows, err = db.QueryContext(ctx, `
			SELECT f.id, t.id, t.kind, t.name, COALESCE(t.year, 0),
			       COALESCE(json_extract(i.numbers, '$.season'), 0),
			       COALESCE(json_extract(i.numbers, '$.episode'), 0),
			       f.relative_path, lf.path, f.size,
			       COALESCE(f.media_info, ''), COALESCE(v.label, '')
			FROM files f
			JOIN slots s ON s.file_id = f.id
			JOIN items i ON i.id = s.item_id
			JOIN versions v ON v.id = f.version_id
			JOIN library_folders lf ON lf.id = v.library_folder_id
			JOIN titles t ON t.id = i.title_id
			WHERE t.kind = 'show' AND t.state != 'removed'
			  AND f.state = 'present' AND f.role = 'main'
			  AND i.kind = 'episode'
			  AND CAST(json_extract(t.source_ids, '$.tmdb') AS TEXT) = ?
			  AND COALESCE(json_extract(i.numbers, '$.season'), 0) = ?
			  AND COALESCE(json_extract(i.numbers, '$.episode'), 0) = ?
			ORDER BY v.is_main DESC, f.size DESC, f.id`, tmdbID, season, episode)
	} else {
		rows, err = db.QueryContext(ctx, `
			SELECT f.id, t.id, t.kind, t.name, COALESCE(t.year, 0),
			       0, 0,
			       f.relative_path, lf.path, f.size,
			       COALESCE(f.media_info, ''), COALESCE(v.label, '')
			FROM files f
			JOIN versions v ON v.id = f.version_id
			JOIN library_folders lf ON lf.id = v.library_folder_id
			JOIN titles t ON t.id = v.title_id
			WHERE t.kind = 'movie' AND t.state != 'removed'
			  AND f.state = 'present' AND f.role = 'main'
			  AND CAST(json_extract(t.source_ids, '$.tmdb') AS TEXT) = ?
			ORDER BY v.is_main DESC, f.size DESC, f.id`, tmdbID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LocalStreamFile
	for rows.Next() {
		var row LocalStreamFile
		var mediaInfoJSON string
		if err := rows.Scan(
			&row.FileID, &row.TitleID, &row.Kind, &row.Name, &row.Year,
			&row.Season, &row.Episode,
			&row.RelPath, &row.RootPath, &row.Size,
			&mediaInfoJSON, &row.VersionLabel,
		); err != nil {
			return nil, err
		}
		full, ok := resolveContainedMediaFile(row.RootPath, row.RelPath)
		if !ok {
			continue
		}
		row.FullPath = full
		row.Quality, row.Codec, row.Container = parseStreamMediaInfo(mediaInfoJSON, row.RelPath)
		out = append(out, row)
	}
	return out, rows.Err()
}

// FindLocalVideoByID resolves one present main file from the Library by its
// database file id, checking root-folder containment before returning it.
func FindLocalVideoByID(ctx context.Context, db database.Querier, fileID int64) (LocalStreamFile, error) {
	if db == nil || fileID <= 0 {
		return LocalStreamFile{}, ErrLocalFileUnavailable
	}
	var row LocalStreamFile
	var mediaInfoJSON string
	err := db.QueryRowContext(ctx, `
		SELECT f.id, t.id, t.kind, t.name, COALESCE(t.year, 0),
		       f.relative_path, lf.path, f.size,
		       COALESCE(f.media_info, ''), COALESCE(v.label, '')
		FROM files f
		JOIN versions v ON v.id = f.version_id
		JOIN library_folders lf ON lf.id = v.library_folder_id
		JOIN titles t ON t.id = v.title_id
		WHERE f.id = ? AND t.state != 'removed'
		  AND f.state = 'present' AND f.role = 'main'
		LIMIT 1`, fileID).Scan(
		&row.FileID, &row.TitleID, &row.Kind, &row.Name, &row.Year,
		&row.RelPath, &row.RootPath, &row.Size,
		&mediaInfoJSON, &row.VersionLabel,
	)
	if err != nil {
		return LocalStreamFile{}, ErrLocalFileUnavailable
	}
	full, ok := resolveContainedMediaFile(row.RootPath, row.RelPath)
	if !ok {
		return LocalStreamFile{}, ErrLocalFileUnavailable
	}
	row.FullPath = full
	row.Quality, row.Codec, row.Container = parseStreamMediaInfo(mediaInfoJSON, row.RelPath)
	return row, nil
}

func resolveContainedMediaFile(root, rel string) (string, bool) {
	root = strings.TrimSpace(root)
	rel = strings.TrimSpace(rel)
	if root == "" || rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	cleanRoot := filepath.Clean(root)
	cand := filepath.Clean(filepath.Join(cleanRoot, rel))
	diff, err := filepath.Rel(cleanRoot, cand)
	if err != nil || diff == "." || diff == ".." || strings.HasPrefix(diff, ".."+string(filepath.Separator)) || filepath.IsAbs(diff) {
		return "", false
	}
	realRoot, errRoot := filepath.EvalSymlinks(cleanRoot)
	realCand, errCand := filepath.EvalSymlinks(cand)
	if errRoot != nil || errCand != nil {
		return "", false
	}
	realDiff, err := filepath.Rel(realRoot, realCand)
	if err != nil || realDiff == "." || realDiff == ".." || strings.HasPrefix(realDiff, ".."+string(filepath.Separator)) || filepath.IsAbs(realDiff) {
		return "", false
	}
	st, err := os.Stat(realCand)
	if err != nil || st.IsDir() || !st.Mode().IsRegular() {
		return "", false
	}
	return realCand, true
}

func parseStreamMediaInfo(raw, relPath string) (quality, codec, container string) {
	if raw != "" {
		var m struct {
			VideoCodec  string `json:"video_codec"`
			Container   string `json:"container"`
			Resolution  string `json:"resolution"`
			VideoWidth  int    `json:"video_width"`
			VideoHeight int    `json:"video_height"`
		}
		if json.Unmarshal([]byte(raw), &m) == nil {
			codec = strings.ToUpper(strings.TrimSpace(m.VideoCodec))
			container = strings.ToLower(strings.TrimSpace(m.Container))
			switch {
			case m.VideoHeight >= 2000 || m.VideoWidth >= 3800:
				quality = "2160p"
			case m.VideoHeight >= 1000 || m.VideoWidth >= 1900:
				quality = "1080p"
			case m.VideoHeight >= 700 || m.VideoWidth >= 1200:
				quality = "720p"
			case m.VideoHeight > 0:
				quality = "480p"
			default:
				quality = strings.TrimSpace(m.Resolution)
			}
		}
	}
	lower := strings.ToLower(relPath)
	if quality == "" {
		for _, q := range []string{"2160p", "4k", "1080p", "720p", "480p"} {
			if strings.Contains(lower, q) {
				if q == "4k" {
					quality = "2160p"
				} else {
					quality = q
				}
				break
			}
		}
	}
	if container == "" {
		container = strings.TrimPrefix(strings.ToLower(filepath.Ext(relPath)), ".")
	}
	return quality, codec, container
}
