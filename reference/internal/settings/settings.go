// Package settings — настройки сайта (site.yaml: settings): одна запись в site_settings (id = 1) — название
// и описание, картинка для соцсетей по умолчанию, контакты и ссылки на соцсети, баннер cookies, коды
// подтверждения сайта. Правки видны на сайте сразу, черновика нет. Здесь — запросы, чтение с подстановкой по умолчанию и раздел
// админки; на сайт данные выводит cmd/site (siteLoader → web.Site).
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/keelkit/keelkit/reference/internal/listfield"
)

// DefaultName — название сайта, пока в site_settings нет строки (до seed или сохранения настроек в админке).
const DefaultName = "Keelkit reference"

// Settings — настройки сайта; пустое поле на сайте не выводится.
type Settings struct {
	Name        string
	Description string // описание сайта: meta description страниц без своего, RSS
	ShareImage  string // id изображения для og:image страниц без своей картинки; пусто — без неё
	Phone       string
	Email       string
	Address     string
	Social      []Social // ссылки на соцсети в подвале по порядку (поле-список, internal/listfield)
	CookieText  string   // текст баннера cookies; пусто — баннера нет
	CookieLink  string   // «Подробнее» в баннере: путь на сайте или адрес http(s)
	Yandex      string   // код подтверждения Яндекс.Вебмастера (мета-тег yandex-verification)
	Google      string   // код подтверждения Google Search Console (мета-тег google-site-verification)
}

// SocialField — поле настроек со ссылками на соцсети: имя в форме и ключ ошибок (listfield).
const SocialField = "social"

// Social — ссылка на соцсеть: элемент поля-списка social (JSON в колонке site_settings.social).
type Social struct {
	ID   string `json:"id"`
	Name string `json:"name"` // как подписать ссылку: «Telegram», «ВКонтакте»
	URL  string `json:"url"`  // адрес http(s)
}

func socialID(s *Social) *string { return &s.ID }

// Get — настройки сайта; строки ещё нет — только DefaultName.
func Get(ctx context.Context, q *Queries) (Settings, error) {
	s, err := q.GetSiteSettings(ctx)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Settings{Name: DefaultName}, nil
	case err != nil:
		return Settings{}, fmt.Errorf("site settings: %w", err)
	}
	social, err := listfield.Decode(s.Social, socialID)
	if err != nil {
		return Settings{}, fmt.Errorf("site settings: social: %w", err)
	}
	return Settings{
		Name: s.Name, Description: s.Description, ShareImage: s.ShareImage.String,
		Phone: s.Phone, Email: s.Email, Address: s.Address, Social: social,
		CookieText: s.CookieText, CookieLink: s.CookieLink,
		Yandex: s.YandexVerification, Google: s.GoogleVerification,
	}, nil
}

// save записывает настройки (строки нет — создаёт).
func save(ctx context.Context, q *Queries, s Settings) error {
	social, err := listfield.Encode(s.Social)
	if err != nil {
		return err
	}
	return q.SaveSiteSettings(ctx, SaveSiteSettingsParams{
		Name: s.Name, Description: s.Description,
		ShareImage: sql.NullString{String: s.ShareImage, Valid: s.ShareImage != ""},
		Phone:      s.Phone, Email: s.Email, Address: s.Address, Social: social,
		CookieText: s.CookieText, CookieLink: s.CookieLink,
		YandexVerification: s.Yandex, GoogleVerification: s.Google,
	})
}

// UseImage — изображение выбрано картинкой для соцсетей (медиатека не даёт удалить используемое).
func UseImage(ctx context.Context, q *Queries, imageID string) (bool, error) {
	return q.SettingsUseImage(ctx, sql.NullString{String: imageID, Valid: true})
}
