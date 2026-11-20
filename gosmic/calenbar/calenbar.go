package calenbar

import (
	"net/http"

	"anto.pt/x/gosmic/httpx"
)

type Website struct{}

var _ httpx.Website = Website{}

func (Website) Register(devmode bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Write([]byte(`
<h1>Calenbar privacy policy</h1>
<p>Calenbar accesses your Google account email, account identifier, calendars and events to show your agenda. Calendar access is read-only.</p>
<p>Calenbar communicates directly with Google. Calendar events are kept in memory on your Mac; account details are stored locally, and refresh tokens are stored in macOS Keychain. Calenbar does not send this information to the developer.</p>
<p>Remove an account in Calenbar to delete its locally stored account details and token. You can also revoke access in your <a href="https://myaccount.google.com/connections">Google Account settings</a>.</p>
<p>Calenbar’s use of Google user data complies with the <a href="https://developers.google.com/terms/api-services-user-data-policy">Google API Services User Data Policy</a>, including its Limited Use requirements.</p>
<p>For privacy questions, contact <a href="mailto:antonio@pitasi.dev">antonio@pitasi.dev</a>.</p>
			`))
	})
}
