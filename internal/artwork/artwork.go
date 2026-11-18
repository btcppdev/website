// Package artwork resolves event uploads and bundled artwork consistently.
package artwork

import (
	"btcpp-web/internal/types"
	"os"
	"path/filepath"
	"strings"
)

const Placeholder = "/static/img/rebrand/light-sketch-bg.avif"

type Images struct{ Original, AVIF string }

func (a Images) Web() string {
	if a.AVIF != "" {
		return a.AVIF
	}
	return a.Original
}

// Resolve never mixes an uploaded original with an older bundled AVIF.
func Resolve(conf *types.Conf, base string) Images {
	if conf == nil {
		return Images{}
	}
	var original, avif string
	switch base {
	case "leading":
		original, avif = conf.LeadingImageURL, conf.LeadingImageAVIFURL
	case "logo-black":
		original = conf.LogoBlackURL
	case "logo-white":
		original = conf.LogoWhiteURL
	}
	if original != "" {
		return Images{Original: original, AVIF: avif}
	}
	return Bundled(conf.Tag, base)
}

func Bundled(tag, base string) Images {
	tag, base = strings.TrimSpace(tag), strings.TrimSpace(base)
	if tag == "" || base == "" || strings.ContainsAny(tag+base, "/\\") || tag == "." || tag == ".." || base == "." || base == ".." {
		return Images{}
	}
	a := Images{}
	for _, ext := range []string{"png", "jpg", "jpeg", "webp", "svg", "avif"} {
		path := filepath.Join("static", "img", tag, base+"."+ext)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			url := "/" + filepath.ToSlash(path)
			if ext == "avif" {
				a.AVIF = url
			} else if a.Original == "" {
				a.Original = url
			}
		}
	}
	if a.Original == "" && a.AVIF == "" && base == "leading" {
		a.Original = map[string]string{"atx22": "/static/img/atx22/leading.png", "atx24": "/static/img/atx24.png", "atx25": "/static/img/atx25_promo.png", "cdmx22": "/static/img/cdmx22/leading.png", "berlin24": "/static/img/berlin24/leading.png", "berlin25": "/static/img/berlin/leading.png", "floripa": "/static/img/floripa/exterior_one.avif", "istanbul": "/static/img/istanbul/leading.png"}[tag]
	}
	return a
}

// Email prefers the original PNG/JPEG rather than the web derivative.
func Email(baseURI string, conf *types.Conf) string {
	a := Resolve(conf, "leading")
	src := a.Original
	if src == "" && conf != nil {
		src = "/static/img/" + conf.Tag + "/leading.png"
	}
	return Absolute(baseURI, src)
}
func Absolute(baseURI, src string) string {
	if strings.HasPrefix(src, "/") {
		return strings.TrimRight(baseURI, "/") + src
	}
	return src
}
