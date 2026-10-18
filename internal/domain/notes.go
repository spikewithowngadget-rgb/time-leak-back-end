package domain

import (
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	NoteDateSelected  = "selected"
	NoteDateCreatedAt = "created_at"
)

// NoteMetadata deliberately excludes the owner and file URLs.
type NoteMetadata struct {
	ID             string    `json:"id"`
	NoteType       string    `json:"note_type"`
	NoteDate       string    `json:"note_date"`
	NoteDateSource string    `json:"note_date_source"`
	FileCount      int       `json:"file_count"`
	FileType       string    `json:"file_type"`
	CreatedAt      time.Time `json:"createdAt"`
}

type NoteListFilter struct {
	From         string
	To           string
	Page         int
	Limit        int
	MetadataOnly bool
}

func (n Note) Metadata() NoteMetadata {
	return NoteMetadata{
		ID: n.ID, NoteType: n.NoteType, NoteDate: n.NoteDate,
		NoteDateSource: n.NoteDateSource, FileCount: n.FileCount,
		FileType: n.FileType, CreatedAt: n.CreatedAt,
	}
}

// ValidNoteDate validates a civil calendar date, independent of timezone.
func ValidNoteDate(value string) bool {
	if len(value) != len(time.DateOnly) {
		return false
	}
	t, err := time.Parse(time.DateOnly, value)
	return err == nil && t.Year() >= 1 && t.Format(time.DateOnly) == value
}

// NoteFileMetadata uses the same nonblank paths that are persisted by the
// repository. Classification depends on the first file, including mixed notes.
func NoteFileMetadata(files []string) (count int, fileType string) {
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		count++
		if count != 1 {
			continue
		}
		if parsed, err := url.Parse(file); err == nil {
			file = parsed.Path
		}
		switch strings.ToLower(path.Ext(strings.ReplaceAll(file, "\\", "/"))) {
		case ".jpg", ".jpeg", ".png", ".heic", ".webp", ".gif", ".bmp":
			fileType = "image"
		case ".mp4", ".mov", ".avi", ".mkv", ".webm", ".m4v":
			fileType = "video"
		case ".m4a", ".mp3", ".wav", ".aac", ".ogg", ".flac":
			fileType = "audio"
		default:
			fileType = "document"
		}
	}
	return count, fileType
}
