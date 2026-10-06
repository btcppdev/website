package emails

import (
	"fmt"
	"strings"

	"btcpp-web/internal/i18n"
	"btcpp-web/internal/types"
)

var registrationEmailCatalog, registrationEmailCatalogErr = i18n.Load()

// Preserve the complete organizer-edited English message. The Korean section
// supplies ticket/event essentials without pretending to translate custom copy.
func registrationEmailBody(locale, kind string, conf *types.Conf, dashboard, uri, doors string, english []byte) ([]byte, error) {
	if types.RegistrationLocale(locale) != "ko" || conf == nil {
		return english, nil
	}
	if registrationEmailCatalogErr != nil {
		return nil, registrationEmailCatalogErr
	}
	if !registrationEmailCatalog.Supports(conf.Tag, "ko") {
		return english, nil
	}
	plain := strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "`", "\\`")
	text := func(s string) string { return plain.Replace(registrationEmailCatalog.Content(conf.Tag, "ko", s)) }
	values := map[string]any{
		"Event": text(conf.Desc), "Date": text(conf.DateDesc), "Venue": text(conf.Venue), "Location": text(conf.Location),
		"Dashboard": dashboard, "EventURL": strings.TrimRight(uri, "/") + registrationEmailCatalog.URL("ko", "/"+conf.Tag),
	}
	header, err := registrationEmailCatalog.Message("ko", "email."+kind+".intro", values)
	if err != nil {
		return nil, err
	}
	if doors != "" {
		line, err := registrationEmailCatalog.Message("ko", "email.doors", map[string]any{"Time": plain.Replace(doors)})
		if err != nil {
			return nil, err
		}
		header += "\n\n" + line
	}
	ending, err := registrationEmailCatalog.Message("ko", "email.english_follows")
	if err != nil {
		return nil, err
	}
	// Newsletter style metadata must remain at the start of the document.
	prefix := ""
	original := string(english)
	if strings.HasPrefix(original, "---\n") {
		if boundary := strings.Index(original[4:], "\n---\n"); boundary >= 0 {
			end := 4 + boundary + len("\n---\n")
			prefix, original = original[:end], original[end:]
		}
	}
	return []byte(fmt.Sprintf("%s%s\n\n%s\n\n---\n\n## English\n\n%s", prefix, header, ending, original)), nil
}
