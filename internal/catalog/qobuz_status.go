package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cesargomez89/navidrums/internal/constants"
)

// qobuzProbeTrackID is a long-standing Qobuz catalogue track used only to test
// whether a signature is accepted. Nothing is downloaded: the response status
// alone distinguishes a stale app secret from a stale auth token.
const qobuzProbeTrackID = "19512574"

// QobuzCredentialState describes one credential's health.
type QobuzCredentialState string

const (
	QobuzStateOK        QobuzCredentialState = "ok"
	QobuzStateMissing   QobuzCredentialState = "missing"
	QobuzStateRejected  QobuzCredentialState = "rejected"
	QobuzStateUnchecked QobuzCredentialState = "unchecked"
)

// QobuzStatus reports whether the configured Qobuz credentials still work.
// app_id/app_secret are lifted from the web player bundle and are rotated by
// Qobuz, and an auth token eventually expires, so each is reported separately:
// a rejected secret breaks downloads while browsing keeps working.
type QobuzStatus struct {
	AppID       QobuzCredentialState `json:"app_id"`
	Account     QobuzCredentialState `json:"account"`
	AppSecret   QobuzCredentialState `json:"app_secret"`
	CanBrowse   bool                 `json:"can_browse"`
	CanDownload bool                 `json:"can_download"`
	Message     string               `json:"message"`
	CheckedAt   time.Time            `json:"checked_at"`
}

// CheckCredentials probes Qobuz and reports which credentials still work.
func (p *QobuzDirectProvider) CheckCredentials(ctx context.Context) *QobuzStatus {
	status := &QobuzStatus{
		AppID:     QobuzStateOK,
		Account:   QobuzStateUnchecked,
		AppSecret: QobuzStateUnchecked,
		CheckedAt: time.Now(),
	}

	if p.creds.AppID == "" {
		status.AppID = QobuzStateMissing
		status.Account = QobuzStateMissing
		status.Message = "QOBUZ_APP_ID is not set."
		return status
	}

	if !p.creds.CanAuthenticate() {
		status.Account = QobuzStateMissing
		status.Message = "Set an auth token, or an email and password, to identify the account."
		return status
	}

	// Step 1: can we authenticate at all? This covers an expired token and a
	// changed password alike.
	token, err := p.authToken(ctx, false)
	if err != nil {
		status.Account = QobuzStateRejected
		switch {
		case errors.Is(err, ErrQobuzIneligible):
			status.Message = "Signed in, but this account has no paid subscription."
		case errors.Is(err, ErrQobuzTokenRejected):
			status.Message = "The auth token was rejected. Grab a fresh one from the web player."
		default:
			status.Message = "Could not authenticate: " + err.Error()
		}
		return status
	}

	// A token supplied directly is never exercised by authToken, so verify it
	// against an endpoint that requires one but needs no signature. Otherwise an
	// expired token reads as healthy until a download fails for another reason.
	if code, probeErr := p.probeAccount(ctx, token); code == http.StatusUnauthorized {
		status.Account = QobuzStateRejected
		status.Message = "Qobuz rejected the account credentials. If you configured an auth token, grab a fresh one from the web player."
		return status
	} else if code == 0 && probeErr != nil {
		status.Account = QobuzStateUnchecked
		status.Message = "Could not reach Qobuz to verify the account: " + probeErr.Error()
		return status
	}

	status.Account = QobuzStateOK
	status.CanBrowse = true

	if !p.creds.CanSign() {
		status.AppSecret = QobuzStateMissing
		status.Message = "Browsing works. Set the app secret to enable downloads."
		return status
	}

	// Step 2: probe a signed request. Qobuz answers 400 when the signature does
	// not verify, which is what a rotated app secret looks like.
	code, err := p.probeSignature(ctx, token)
	switch {
	case err != nil && code == 0:
		status.AppSecret = QobuzStateUnchecked
		status.Message = "Could not reach Qobuz to verify the app secret: " + err.Error()
	case code == http.StatusOK:
		status.AppSecret = QobuzStateOK
		status.CanDownload = true
		status.Message = "Browsing and downloads are working."
	case code == http.StatusBadRequest:
		status.AppSecret = QobuzStateRejected
		status.Message = "The app secret was rejected, so downloads will fail. Re-read it from the web player bundle."
	case code == http.StatusUnauthorized:
		status.Account = QobuzStateRejected
		status.CanBrowse = false
		status.Message = "Qobuz rejected the account credentials."
	default:
		status.AppSecret = QobuzStateUnchecked
		status.Message = "Unexpected response from Qobuz: " + strconv.Itoa(code)
	}

	return status
}

// probeSignature issues a signed file-URL request and returns the HTTP status
// without consuming the result.
func (p *QobuzDirectProvider) probeSignature(ctx context.Context, token string) (int, error) {
	formatID := qobuzDirectFormatID(constants.QualityLossless)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	params := url.Values{}
	params.Set("request_ts", timestamp)
	params.Set("request_sig", qobuzRequestSignature(formatID, qobuzProbeTrackID, timestamp, p.creds.AppSecret))
	params.Set("track_id", qobuzProbeTrackID)
	params.Set("format_id", strconv.Itoa(formatID))
	params.Set("intent", "stream")

	var resp QobuzFileURLResponse
	err := p.get(ctx, "track/getFileUrl", params, token, &resp)
	if err == nil {
		return http.StatusOK, nil
	}
	if code := statusCodeOf(err); code != 0 {
		return code, err
	}
	return 0, err
}

// probeAccount calls an endpoint that requires a valid auth token but no
// request signature, isolating account problems from app-secret problems.
func (p *QobuzDirectProvider) probeAccount(ctx context.Context, token string) (int, error) {
	params := url.Values{}
	params.Set("type", "tracks")
	params.Set("limit", "1")

	var discard map[string]any
	err := p.get(ctx, "favorite/getUserFavorites", params, token, &discard)
	if err == nil {
		return http.StatusOK, nil
	}
	if code := statusCodeOf(err); code != 0 {
		return code, err
	}
	return 0, err
}
