package imgproc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestArtworkAVIFDimensions(t *testing.T) {
	requireFFmpeg(t)
	for _, size := range []image.Point{{2400, 1200}, {320, 640}} {
		t.Run(size.String(), func(t *testing.T) {
			img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
			for y := 0; y < size.Y; y++ {
				for x := 0; x < size.X; x++ {
					img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
				}
			}
			var original bytes.Buffer
			if err := png.Encode(&original, img); err != nil {
				t.Fatal(err)
			}
			encoded, err := MakeArtworkAVIF(original.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "artwork.avif")
			if err = os.WriteFile(path, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			decoded, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-frames:v", "1", "-f", "image2pipe", "-c:v", "png", "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			config, err := png.DecodeConfig(bytes.NewReader(decoded))
			if err != nil {
				t.Fatal(err)
			}
			want := size
			if want.X > 1920 {
				want.Y = want.Y * 1920 / want.X
				want.X = 1920
			}
			if config.Width != want.X || config.Height != want.Y {
				t.Fatalf("distorted or upscaled: %dx%d, want %v", config.Width, config.Height, want)
			}
			t.Logf("%dx%d PNG %d bytes -> AVIF %d bytes", size.X, size.Y, original.Len(), len(encoded))
		})
	}
}
