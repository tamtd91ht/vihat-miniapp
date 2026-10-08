package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// POST /api/v1/client-errors — the Mini App reports a FAILED Zalo SDK call, and this server writes
// one log line so the failure can be read in Rancher (owner, 08/10/2026).
//
// WHY IT EXISTS: zmp-sdk answers -1401 (UNAUTHORIZED) for at least three different causes of a failed
// `getAccessToken` — "User Authentication Required", "Zalo app has not been activated", "Can't get
// accessToken" (`zmp-sdk/apis/common/token.js`, 2.53.0). Only the SDK's `message` tells them apart, and
// it only exists on the citizen's phone. Without this route nobody can see which one a real phone hit.
//
// PUBLIC on purpose: the failure being reported is the one that prevents a session, so there is nothing
// to authenticate with. What guards it: 30 reports / hour / IP (owner's threshold), a closed set of
// `capability` values, fixed-shape `appId`/`host`, and a message cut to 200 characters with control
// characters removed — a client cannot forge extra log lines or flood the log.
//
// NOTHING PERSONAL GOES IN OR OUT: the body has no field for a name, a number or a token, and unknown
// fields are ignored. The client IP is not logged either. Nothing is stored — a log line only.
const (
	clientErrorMaxReports = 30
	clientErrorWindow     = time.Hour
	clientErrorMaxMessage = 200

	// clientErrorLogPrefix is the fixed text to search for in Rancher (owner asked for a prefix).
	clientErrorLogPrefix = "[ZALO_SDK_ERROR]"
)

// clientErrorCapabilities mirrors `ZaloCapability` in citizen-app `src/features/tinh-nang/zalo-api.ts`.
// A value outside this set is logged as "unknown", never verbatim.
var clientErrorCapabilities = map[string]bool{
	"access-token": true, "phone": true, "location": true, "name": true,
	"camera": true, "photos": true, "other": true,
}

var (
	clientErrorAppID = regexp.MustCompile(`^[0-9]{1,32}$`)
	clientErrorHost  = regexp.MustCompile(`^[a-z0-9.-]{1,253}$`)
)

type clientErrorReport struct {
	Capability string `json:"capability"`
	Code       *int   `json:"code"`
	Message    string `json:"message"`
	AppID      string `json:"appId"`
	Host       string `json:"host"`
}

func (s *Server) clientErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if ok, first := s.clientErrorLimit.Cho(khoaGioiHan(ipCuaKhach(r))); !ok {
		if first {
			// One line per blocked burst — see GioiHanIP.Cho.
			s.log.Warn(clientErrorLogPrefix + " rate limited")
		}
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	var rep clientErrorReport
	if err := json.NewDecoder(io.LimitReader(r.Body, gioiHanThan)).Decode(&rep); err != nil || rep.Code == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	capability := rep.Capability
	if !clientErrorCapabilities[capability] {
		capability = "unknown"
	}
	appID := rep.AppID
	if !clientErrorAppID.MatchString(appID) {
		appID = ""
	}
	host := strings.ToLower(rep.Host)
	if !clientErrorHost.MatchString(host) {
		host = ""
	}

	s.log.Warn(clientErrorLogPrefix+" "+capability,
		"capability", capability,
		"code", *rep.Code,
		"sdk_message", cleanClientMessage(rep.Message),
		"app_id", appID,
		"host", host,
	)
	w.WriteHeader(http.StatusNoContent)
}

// cleanClientMessage keeps at most 200 characters and drops control characters, so a client cannot
// break the line into several fake log entries.
func cleanClientMessage(m string) string {
	var b strings.Builder
	n := 0
	for _, c := range m {
		if n == clientErrorMaxMessage {
			break
		}
		if unicode.IsControl(c) {
			continue
		}
		b.WriteRune(c)
		n++
	}
	return b.String()
}
