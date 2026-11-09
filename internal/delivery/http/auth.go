package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Identity — пользователь, подтверждённый подписью initData Mini App.
type Identity struct {
	Username     string
	FirstName    string
	LastName     string
	LanguageCode string
	// StartParam — параметр startapp из ссылки t.me/<bot>/<app>?startapp=...
	StartParam string
	TelegramID int64
}

// Ошибки проверки initData.
var (
	ErrMissingInitData = errors.New("missing telegram init data")
	ErrBadSignature    = errors.New("invalid telegram init data signature")
	ErrExpiredInitData = errors.New("telegram init data expired")
)

// initDataTTL ограничивает окно переигрывания перехваченной подписи.
const initDataTTL = 24 * time.Hour

type identityKey struct{}

// IdentityFrom достаёт пользователя, положенного TelegramAuth.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

// TelegramAuth проверяет подпись initData (заголовок Authorization: tma <initData>).
// onUser вызывается для каждого подтверждённого пользователя (учёт в players).
func TelegramAuth(botToken string, onUser func(context.Context, Identity)) func(http.Handler) http.Handler {
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	secretKey := secret.Sum(nil)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := initDataFrom(r)
			if raw == "" {
				writeError(w, http.StatusUnauthorized, "missing_init_data", "Требуется авторизация через Telegram")
				return
			}

			id, err := ParseInitData(raw, secretKey, time.Now())
			if err != nil {
				code := "invalid_init_data"
				if errors.Is(err, ErrExpiredInitData) {
					code = "init_data_expired"
				}
				writeError(w, http.StatusUnauthorized, code, "Требуется авторизация через Telegram")
				return
			}

			if onUser != nil {
				onUser(r.Context(), id)
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
		})
	}
}

func initDataFrom(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "tma ") {
		return strings.TrimPrefix(auth, "tma ")
	}
	return r.Header.Get("X-Telegram-Init-Data")
}

// ParseInitData проверяет подпись и срок годности initData.
func ParseInitData(raw string, secretKey []byte, now time.Time) (Identity, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return Identity{}, ErrBadSignature
	}

	provided := values.Get("hash")
	if provided == "" {
		return Identity{}, ErrBadSignature
	}
	values.Del("hash")

	// signature (Ed25519, Bot API 8.0) одни клиенты включают в строку проверки, другие нет.
	valid := hashMatches(values, secretKey, provided)
	if !valid && values.Has("signature") {
		values.Del("signature")
		valid = hashMatches(values, secretKey, provided)
	}
	if !valid {
		return Identity{}, ErrBadSignature
	}

	ts, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return Identity{}, ErrBadSignature
	}
	if now.Sub(time.Unix(ts, 0)) > initDataTTL {
		return Identity{}, ErrExpiredInitData
	}

	var u struct {
		Username     string `json:"username"`
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		LanguageCode string `json:"language_code"`
		ID           int64  `json:"id"`
	}
	if err := json.Unmarshal([]byte(values.Get("user")), &u); err != nil || u.ID == 0 {
		return Identity{}, ErrBadSignature
	}

	return Identity{
		TelegramID:   u.ID,
		Username:     u.Username,
		FirstName:    u.FirstName,
		LastName:     u.LastName,
		LanguageCode: u.LanguageCode,
		StartParam:   values.Get("start_param"),
	}, nil
}

func hashMatches(values url.Values, secretKey []byte, provided string) bool {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k + "=" + values.Get(k))
	}

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(b.String()))

	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(provided))
}
