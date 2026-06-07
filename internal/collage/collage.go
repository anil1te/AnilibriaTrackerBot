package collage

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fogleman/gg"
	"github.com/nfnt/resize"
)

const (
	ImgWidth  = 300
	ImgHeight = 420
	Padding   = 10
	Cols      = 3
)

type PosterData struct {
	Index int
	Img   image.Image
}

func GenerateScheduleCollage(posterURLs []string, startIndex int) ([]byte, error) {
	if len(posterURLs) == 0 {
		return nil, fmt.Errorf("no images to process")
	}

	// Download images concurrently
	var wg sync.WaitGroup
	posters := make([]image.Image, len(posterURLs))
	errs := make([]error, len(posterURLs))

	for i, urlStr := range posterURLs {
		if urlStr == "" {
			continue
		}
		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()
			
			cacheDir := ".cache/posters"
			os.MkdirAll(cacheDir, 0755)

			// Simple safe filename based on URL path
			parts := strings.Split(url, "/")
			filename := parts[len(parts)-1]
			cachePath := filepath.Join(cacheDir, filename)

			var img image.Image
			var err error

			if file, errOpen := os.Open(cachePath); errOpen == nil {
				img, _, err = image.Decode(file)
				file.Close()
			} else {
				resp, errGet := http.Get("https://www.anilibria.top" + url)
				if errGet != nil {
					errs[index] = errGet
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					buf, _ := io.ReadAll(resp.Body)
					os.WriteFile(cachePath, buf, 0644)
					img, _, err = image.Decode(bytes.NewReader(buf))
				} else {
					errs[index] = fmt.Errorf("bad status code: %d", resp.StatusCode)
					return
				}
			}

			if err == nil && img != nil {
				// Resize to fixed dimensions
				img = resize.Resize(ImgWidth, ImgHeight, img, resize.Lanczos3)
				posters[index] = img
			} else if err != nil {
				errs[index] = err
			}
		}(i, urlStr)
	}
	wg.Wait()

	// Calculate canvas size
	rows := int(math.Ceil(float64(len(posterURLs)) / float64(Cols)))
	canvasW := Cols*ImgWidth + (Cols+1)*Padding
	canvasH := rows*ImgHeight + (rows+1)*Padding

	dc := gg.NewContext(canvasW, canvasH)
	
	// Background
	dc.SetColor(color.RGBA{20, 20, 20, 255})
	dc.Clear()

	// Load font
	if err := dc.LoadFontFace("Roboto-Bold.ttf", 48); err != nil {
		fmt.Println("Warning: Could not load font:", err)
	}

	// Draw each poster
	for i, img := range posters {
		if img == nil {
			continue // Skip failed downloads
		}
		
		col := i % Cols
		row := i / Cols

		x := Padding + col*(ImgWidth+Padding)
		y := Padding + row*(ImgHeight+Padding)

		// Draw image
		dc.DrawImage(img, x, y)

		// Draw circle background for number
		cx := x + 40
		cy := y + 40
		dc.DrawCircle(float64(cx), float64(cy), 30)
		dc.SetColor(color.RGBA{0, 0, 0, 180})
		dc.Fill()

		// Draw number
		dc.SetColor(color.RGBA{255, 255, 255, 255})
		text := fmt.Sprintf("%d", startIndex+i+1)
		dc.DrawStringAnchored(text, float64(cx), float64(cy), 0.5, 0.5)
	}

	var buf bytes.Buffer
	if err := dc.EncodePNG(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
