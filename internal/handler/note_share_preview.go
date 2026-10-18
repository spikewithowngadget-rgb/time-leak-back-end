package handler

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func (h *Handler) SharedNotePreview(w http.ResponseWriter, r *http.Request) {
	if !h.validShareSignature(r, "preview") {
		writeErrorJSON(w, http.StatusForbidden, "invalid or expired file URL")
		return
	}
	note, _, err := h.app.GetSharedNote(r.Context(), r.PathValue("token"), false)
	if err != nil {
		h.writeShareError(w, err)
		return
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 600, 315))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{38, 57, 120, 255}), image.Point{}, draw.Src)
	var source image.Image
	if note.FileType == "image" && len(note.NoteFiles) > 0 {
		if absolute, ok := h.noteFileAbsolutePath(note.NoteFiles[0]); ok {
			if file, err := os.Open(absolute); err == nil {
				defer file.Close()
				cfg, _, err := image.DecodeConfig(io.LimitReader(file, 32<<20))
				if err == nil && cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) <= 20_000_000 {
					if _, err := file.Seek(0, io.SeekStart); err == nil {
						source, _, _ = image.Decode(io.LimitReader(file, 32<<20))
					}
				}
			}
			if source == nil {
				source = h.extraImageThumbnail(r.Context(), absolute)
			}
		}
	}
	if source != nil {
		bounds := source.Bounds()
		width, height := 600, bounds.Dy()*600/bounds.Dx()
		if height > 315 {
			height = 315
			width = bounds.Dx() * 315 / bounds.Dy()
		}
		if width < 1 {
			width = 1
		}
		if height < 1 {
			height = 1
		}
		x0, y0 := (600-width)/2, (315-height)/2
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				canvas.Set(x0+x, y0+y, source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height))
			}
		}
	} else {
		white := image.NewUniform(color.RGBA{237, 241, 255, 255})
		rect := func(x0, y0, x1, y1 int) {
			draw.Draw(canvas, image.Rect(x0, y0, x1, y1), white, image.Point{}, draw.Src)
		}
		circle := func(cx, cy, r int) {
			for y := -r; y <= r; y++ {
				for x := -r; x <= r; x++ {
					if x*x+y*y <= r*r {
						canvas.Set(cx+x, cy+y, white.C)
					}
				}
			}
		}
		switch note.FileType {
		case "audio":
			rect(277, 84, 289, 218)
			rect(289, 84, 354, 98)
			rect(342, 84, 354, 202)
			circle(260, 218, 26)
			circle(325, 202, 26)
		case "video":
			for x := 0; x < 95; x++ {
				half := (95 - x) * 70 / 95
				rect(265+x, 157-half, 266+x, 158+half)
			}
		case "image":
			rect(210, 95, 390, 220)
			draw.Draw(canvas, image.Rect(222, 107, 378, 208), image.NewUniform(color.RGBA{38, 57, 120, 255}), image.Point{}, draw.Src)
			circle(340, 132, 15)
		default:
			rect(240, 70, 360, 245)
			draw.Draw(canvas, image.Rect(258, 100, 342, 110), image.NewUniform(color.RGBA{38, 57, 120, 255}), image.Point{}, draw.Src)
			draw.Draw(canvas, image.Rect(258, 130, 342, 140), image.NewUniform(color.RGBA{38, 57, 120, 255}), image.Point{}, draw.Src)
		}
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = png.Encode(w, canvas)
}

// Use the same installed media tools for image formats outside Go's standard
// decoders (for example WebP and HEIC). Invalid or unsupported files fall back
// to the type icon; the command cannot fetch remote resources.
func (h *Handler) extraImageThumbnail(ctx context.Context, file string) image.Image {
	info, err := os.Stat(file)
	if err != nil || info.Size() > 32<<20 {
		return nil
	}
	binary, err := h.audioBinary(h.cfg.Sharing.FFmpegPath, "ffmpeg")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe",
		"-max_alloc", "67108864", "-threads", "1", "-i", file, "-frames:v", "1", "-vf", "scale=600:315:force_original_aspect_ratio=decrease",
		"-f", "image2pipe", "-c:v", "png", "pipe:1").Output()
	if err != nil {
		return nil
	}
	thumbnail, err := png.Decode(bytes.NewReader(output))
	if err != nil {
		return nil
	}
	return thumbnail
}
