package domain

import "testing"

func TestNoteFileMetadata(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		count int
		kind  string
	}{
		{"empty", nil, 0, ""},
		{"blanks", []string{"", "  "}, 0, ""},
		{"mixed", []string{"", " audio/voice.M4A ", "photo/pic.jpg"}, 2, "audio"},
		{"url", []string{"https://example.test/photo/PIC.JPEG?token=secret#file.mp3"}, 1, "image"},
		{"image", []string{"photo/a.heic"}, 1, "image"},
		{"video", []string{"video/a.MOV"}, 1, "video"},
		{"document", []string{"document/a.docx"}, 1, "document"},
		{"unknown", []string{"document/a.bin"}, 1, "document"},
		{"no extension", []string{"document/a"}, 1, "document"},
		{"existing flac", []string{"audio/a.flac"}, 1, "audio"},
		{"existing bmp", []string{"photo/a.bmp"}, 1, "image"},
		{"existing webm", []string{"video/a.webm"}, 1, "video"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count, kind := NoteFileMetadata(tc.files)
			if count != tc.count || kind != tc.kind {
				t.Fatalf("got (%d, %q), want (%d, %q)", count, kind, tc.count, tc.kind)
			}
		})
	}
}

func TestValidNoteDate(t *testing.T) {
	for _, date := range []string{"2026-10-17", "2028-02-29", "0001-01-01", "9999-12-31"} {
		if !ValidNoteDate(date) {
			t.Errorf("rejected %q", date)
		}
	}
	for _, date := range []string{"", "2026-2-01", "2026-02-29", "2026-13-01", "2026-10-17T00:00:00Z", " 2026-10-17", "0000-01-01"} {
		if ValidNoteDate(date) {
			t.Errorf("accepted %q", date)
		}
	}
}
