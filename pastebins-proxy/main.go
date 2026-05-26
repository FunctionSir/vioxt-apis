package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// "/" suffix must NOT be omitted!
const PasteDebianNetBaseURL = "https://paste.debian.net/plainh/"
const PastebinComBaseURL = "https://pastebin.com/raw/"
const PasteBoxIoBaseURL = "https://pastebox.io/api/paste.php?action=raw&slug="

func ValidatePasteID(id string) bool {
	for _, ch := range id {
		if !strings.ContainsAny(string(ch), "QWERTYUIOPASDFGHJKLZXCVBNMqwertyuiopasdfghjklzxcvbnm1234567890") {
			return false
		}
	}
	return true
}

func DebPastebinProxyHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	defer func() { log.Println("Request", r.URL.String(), "processed in", time.Since(now)) }()
	isBin := false
	w.Header().Set("Access-Control-Allow-Origin", "*")
	baseURL := ""
	switch r.PathValue("Pastebin") {
	case "paste.debian.net":
		baseURL = PasteDebianNetBaseURL
	case "pastebin.com":
		baseURL = PastebinComBaseURL
	case "pastebox.io":
		baseURL = PasteBoxIoBaseURL
	default:
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("PASTEBIN_NOT_SUPPORTED_YET"))
		return
	}
	switch r.PathValue("ContentType") {
	case "html":
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "script-src 'none'") // Important!
	case "plain":
		w.Header().Set("Content-Type", "text/plain")
	case "json":
		w.Header().Set("Content-Type", "application/json")
	case "xml":
		w.Header().Set("Content-Type", "application/xml")
	case "svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case "png":
		w.Header().Set("Content-Type", "image/png")
		isBin = true
	case "jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
		isBin = true
	case "gif":
		w.Header().Set("Content-Type", "image/gif")
		isBin = true
	case "apng":
		w.Header().Set("Content-Type", "image/apng")
		isBin = true
	case "avif":
		w.Header().Set("Content-Type", "image/avif")
		isBin = true
	default:
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("INVALID_CONTENT_TYPE"))
		return
	}
	switch r.URL.Query().Get("b64") {
	case "1", "true":
		isBin = true
	case "0", "false":
		isBin = false
	case "inv", "-2":
		isBin = !isBin
	case "", "auto", "-1":
		// Pass //
	default:
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("INVALID_PARAMETER_B64"))
		return
	}
	pasteID := r.PathValue("PasteID")
	if !ValidatePasteID(pasteID) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("INVALID_PASTE_ID"))
		return
	}
	resp, err := http.DefaultClient.Get(baseURL + pasteID)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("ERR_OCCURRED_WHILE_COMMUNICATING_WITH_UPSTREAM"))
		return
	}
	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write([]byte("UPSTREAM_ERR_" + strconv.Itoa(resp.StatusCode)))
		return
	}
	defer func() { _ = resp.Body.Close() }()
	pasteContentReader := http.MaxBytesReader(w, resp.Body, 32<<20)
	pasteContent, err := io.ReadAll(pasteContentReader)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("INTERNAL_SERVER_ERROR"))
		return
	}
	if isBin {
		decoded, err := base64.StdEncoding.DecodeString(string(pasteContent))
		if err != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusUnsupportedMediaType)
			_, _ = w.Write([]byte("NOT_STD_BASE64_ENCODED_CONTENT"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(decoded)
	} else {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pasteContent)
	}
}

func main() {
	fmt.Println("Pastebins Proxy API Backend")
	fmt.Println("Version: 0.1.0")
	if len(os.Args) <= 1 {
		panic("no enough args")
	}
	http.DefaultClient.Timeout = 30 * time.Second
	http.HandleFunc("GET /{Pastebin}/{ContentType}/{PasteID}", DebPastebinProxyHandler)
	err := http.ListenAndServe(os.Args[1], nil)
	panic(err)
}
