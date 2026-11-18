package handlers

import (
	"btcpp-web/internal/artwork"
	"btcpp-web/internal/types"
	"bytes"
	"errors"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestEventArtworkUploadFallbacks(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 10), G: uint8(y * 20), A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	for _, tc := range []struct {
		name, field                               string
		failEncode, failAVIF, larger, transparent bool
		wantAVIF                                  bool
	}{
		{name: "optimized", field: "leading", wantAVIF: true},
		{name: "conversion failure", field: "leading", failEncode: true},
		{name: "derivative upload failure", field: "leading", failAVIF: true},
		{name: "larger derivative", field: "leading", larger: true},
		{name: "logo", field: "black"},
		{name: "transparent artwork", field: "leading", transparent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := raw
			if tc.transparent {
				var transparent bytes.Buffer
				png.Encode(&transparent, image.NewNRGBA(image.Rect(0, 0, 20, 10)))
				input = transparent.Bytes()
			}
			calls := 0
			got, err := storeEventArtwork("conf", tc.field, "image/png", "png", input, func([]byte) ([]byte, error) {
				calls++
				if tc.failEncode {
					return nil, errors.New("encoder unavailable")
				}
				if tc.larger {
					return bytes.Repeat([]byte{1}, len(input)+1), nil
				}
				return []byte("avif"), nil
			}, func(key string, data []byte, mime, digest string) (string, error) {
				if strings.HasSuffix(key, ".avif") && tc.failAVIF {
					return "", errors.New("upload failed")
				}
				return "https://images.example/" + key, nil
			})
			if err != nil || !strings.HasSuffix(got.Original, ".png") {
				t.Fatalf("original upload lost: %+v %v", got, err)
			}
			if (got.AVIF != "") != tc.wantAVIF {
				t.Fatalf("derivative: %+v", got)
			}
			if (tc.field != "leading" || tc.transparent) && calls != 0 {
				t.Fatal("alpha-sensitive asset converted")
			}
		})
	}
}

func TestEventHeroUsesMatchingUploadVariants(t *testing.T) {
	for _, avif := range []string{"", "https://images.example/new.avif"} {
		conf := &types.Conf{Tag: "seoul", LeadingImageURL: "https://images.example/new.png", LeadingImageAVIFURL: avif}
		tpl, err := template.New("root").Funcs(template.FuncMap{
			"confOriginal": func(tag, base string) string { return artwork.Resolve(conf, base).Original },
			"confAVIF":     func(tag, base string) string { return artwork.Resolve(conf, base).AVIF },
		}).ParseFiles("../../templates/section/conf_hero.tmpl")
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err = tpl.ExecuteTemplate(&out, "conf_hero", conf); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "/static/img/seoul/") || !strings.Contains(out.String(), conf.LeadingImageURL) {
			t.Fatal("template used stale local image", out.String())
		}
		if (strings.Contains(out.String(), "<source")) != (avif != "") {
			t.Fatal("invalid AVIF source", out.String())
		}
	}
}
