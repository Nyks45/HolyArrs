package stream

import (
	"net/http"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/webserver"
	"github.com/HolyArrs/holyarrs/app/backend/internal/subtitles"
)

func styledSubtitleText(raw []byte, name, lang string) string {
	return subtitles.StyledText(raw, name, lang)
}

func writeSubtitlePayload(w http.ResponseWriter, vtt []byte, ass, lang, subFile string) {
	payload := map[string]any{"vtt": string(vtt), "language": lang, "sub_file": subFile}
	if ass != "" {
		payload["ass"] = ass
	}
	webserver.WriteJSON(w, http.StatusOK, payload)
}
