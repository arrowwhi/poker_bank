package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const token = "123:secret"

func sign(t *testing.T, values url.Values) string {
	t.Helper()

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+values.Get(k))
	}

	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))

	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))

	return values.Encode()
}

func initData(t *testing.T, authDate time.Time) string {
	return sign(t, url.Values{
		"auth_date":   {strconv.FormatInt(authDate.Unix(), 10)},
		"user":        {`{"id":42,"username":"ivan","first_name":"Иван"}`},
		"start_param": {"c-1001"},
	})
}

func TestTelegramAuth(t *testing.T) {
	var got Identity

	h := TelegramAuth(token, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = IdentityFrom(r.Context())
	}))

	cases := []struct {
		name   string
		header string
		status int
	}{
		{"valid", "tma " + initData(t, time.Now()), http.StatusOK},
		{"missing", "", http.StatusUnauthorized},
		{"expired", "tma " + initData(t, time.Now().Add(-48*time.Hour)), http.StatusUnauthorized},
		{"tampered", "tma " + strings.Replace(initData(t, time.Now()), "ivan", "evil", 1), http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got = Identity{}
			r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if tc.status == http.StatusOK && (got.TelegramID != 42 || got.Username != "ivan" || got.StartParam != "c-1001") {
				t.Fatalf("identity = %+v", got)
			}
		})
	}
}

func TestParseInitDataExpired(t *testing.T) {
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))

	_, err := ParseInitData(initData(t, time.Now().Add(-25*time.Hour)), secret.Sum(nil), time.Now())
	if !errors.Is(err, ErrExpiredInitData) {
		t.Fatalf("err = %v, want expired", err)
	}
}
