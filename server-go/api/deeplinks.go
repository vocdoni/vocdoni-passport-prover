package api

import (
	"encoding/base64"
	"net/http"
	"net/url"
)

func requestDeepLinkURL(r *http.Request, payloadJSON []byte) string {
	params := url.Values{}
	params.Set("request", base64.RawURLEncoding.EncodeToString(payloadJSON))
	return joinURL(appLinkBaseURL(r), "/passport") + "?" + params.Encode()
}

func petitionDeepLinkURL(r *http.Request, petitionID string) string {
	return petitionDeepLinkURLFromValues(baseURL(r), appLinkBaseURL(r), petitionID)
}
