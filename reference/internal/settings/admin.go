package settings

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/admin"
	"github.com/voffkin/keelkit/reference/internal/listfield"
	"github.com/voffkin/keelkit/reference/internal/web"
)

// AdminPath — раздел настроек в админке.
const AdminPath = "/admin/settings"

// Ограничения полей (символов) и формы.
const (
	maxNameLen        = 100
	maxDescriptionLen = 300
	maxPhoneLen       = 50
	maxEmailLen       = 254
	maxAddressLen     = 300
	maxCookieTextLen  = 1000
	maxLinkLen        = 500
	maxSocialNameLen  = 50
	maxFormSize       = 64 << 10 // байт
	libraryLimit      = 200      // изображений в выборе картинки: последние загруженные
)

// Images — то, что разделу нужно от модуля images: выбранная картинка и последние загруженные.
type Images interface {
	Get(ctx context.Context, id string) (*images.Image, error)
	List(ctx context.Context, limit, offset int) ([]*images.Image, error)
}

// Admin — раздел «Настройки»: одна форма, правки сразу на сайте.
// Маршруты оборачиваются в admin.RequireUser: сессия и CSRF-токен уже проверены.
type Admin struct {
	q        *Queries
	media    Images
	mediaURL string // медиатека: ссылка «загрузите в медиатеку»
}

// NewAdmin создаёт раздел. mediaURL — адрес медиатеки в админке.
func NewAdmin(db DBTX, media Images, mediaURL string) *Admin {
	return &Admin{q: New(db), media: media, mediaURL: mediaURL}
}

// settingsForm — форма настроек.
type settingsForm struct {
	Settings
	Errors   map[string]string
	Flash    string
	Unsaved  bool            // форму изменила кнопка списка (↑, ↓, «Убрать», «Добавить»), но не сохранила
	Image    *images.Image   // выбранная картинка для соцсетей (превью и пункт в выборе); nil — не выбрана или её нет
	Library  []*images.Image // выбор картинки: последние загруженные
	MediaURL string
}

// Edit — GET /admin/settings.
func (a *Admin) Edit(w http.ResponseWriter, r *http.Request) {
	s, err := Get(r.Context(), a.q)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f := settingsForm{Settings: s}
	if r.URL.Query().Has("saved") {
		f.Flash = "Сохранено. Изменения уже на сайте."
	}
	if err := a.env(r.Context(), &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, FormPage(f))
}

// Update — POST /admin/settings. Кнопка — поле action: пусто или "save" сохраняет, действие над списком
// (listfield.ParseAction) возвращает форму изменённой без сохранения (200). Ошибки — 422 (с htmx — только форма),
// успех — 303 на форму с ?saved=1 (с htmx — HX-Redirect).
func (a *Admin) Update(w http.ResponseWriter, r *http.Request) {
	f, ok := a.parse(w, r)
	if !ok {
		return
	}
	if err := a.env(r.Context(), &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if action := r.PostFormValue("action"); action != "" && action != "save" {
		act, ok := listfield.ParseAction(action)
		if ok && act.Field == SocialField {
			f.Social, ok = listfield.Apply(f.Social, func(s Social) string { return s.ID }, act.Op, act.Item,
				func() Social { return Social{ID: listfield.NewID()} })
		} else {
			ok = false
		}
		if !ok {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		f.Unsaved = true
		a.render(w, r, http.StatusOK, f)
		return
	}
	if validate(&f) {
		if err := save(r.Context(), a.q, f.Settings); err != nil {
			admin.ServerError(w, r, err)
			return
		}
		admin.Redirect(w, r, AdminPath+"?saved=1")
		return
	}
	a.render(w, r, http.StatusUnprocessableEntity, f)
}

// render — форма с ошибками или изменённая кнопкой списка: с htmx — только форма, без него — страница.
func (a *Admin) render(w http.ResponseWriter, r *http.Request, status int, f settingsForm) {
	c := FormPage(f)
	if admin.IsHTMX(r) {
		c = Form(f)
	}
	web.Render(w, r, status, c)
}

// Image — POST /admin/settings/image (htmx): поле картинки для соцсетей с превью выбранной (поле share_image).
func (a *Admin) Image(w http.ResponseWriter, r *http.Request) {
	f, ok := a.parse(w, r)
	if !ok {
		return
	}
	if err := a.env(r.Context(), &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, imageField(f))
}

// parse — поля из присланной формы (без проверки); тело больше лимита, не форма или испорченные id элементов
// списка — 400.
func (a *Admin) parse(w http.ResponseWriter, r *http.Request) (settingsForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return settingsForm{}, false
	}
	ids, err := listfield.IDs(r.PostForm, SocialField)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return settingsForm{}, false
	}
	v := r.PostFormValue
	social := make([]Social, 0, len(ids))
	for _, id := range ids {
		social = append(social, Social{ID: id,
			Name: v(listfield.Name(SocialField, id, "name")), URL: v(listfield.Name(SocialField, id, "url"))})
	}
	return settingsForm{Settings: Settings{
		Name: v("name"), Description: v("description"), ShareImage: strings.TrimSpace(v("share_image")),
		Phone: v("phone"), Email: v("email"), Address: v("address"), Social: social,
		CookieText: v("cookie_text"), CookieLink: v("cookie_link"),
		Yandex: v("yandex_verification"), Google: v("google_verification"),
	}}, true
}

// env загружает выбранную картинку и медиатеку для выбора.
func (a *Admin) env(ctx context.Context, f *settingsForm) error {
	f.MediaURL = a.mediaURL
	if f.ShareImage != "" {
		img, err := a.media.Get(ctx, f.ShareImage)
		switch {
		case errors.Is(err, images.ErrNotFound):
		case err != nil:
			return fmt.Errorf("share image: %w", err)
		default:
			f.Image = img
		}
	}
	lib, err := a.media.List(ctx, libraryLimit, 0)
	if err != nil {
		return fmt.Errorf("media library: %w", err)
	}
	f.Library = lib
	return nil
}

// verificationCode — код подтверждения: латиница, цифры, «-» и «_».
var verificationCode = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// metaContent — значение content из вставленного целиком мета-тега.
var metaContent = regexp.MustCompile(`(?i)content\s*=\s*["']([^"']*)["']`)

// validate нормализует поля и проверяет их; ошибки — в f.Errors. Картинка должна быть уже загружена в f.Image.
func validate(f *settingsForm) bool {
	errs := map[string]string{}
	s := &f.Settings

	s.Name = oneLine(s.Name)
	switch {
	case s.Name == "":
		errs["name"] = "Укажите название сайта."
	case utf8.RuneCountInString(s.Name) > maxNameLen:
		errs["name"] = fmt.Sprintf("Название длиннее %d символов.", maxNameLen)
	}
	if s.Description = oneLine(s.Description); utf8.RuneCountInString(s.Description) > maxDescriptionLen {
		errs["description"] = fmt.Sprintf("Описание длиннее %d символов.", maxDescriptionLen)
	}
	if s.ShareImage != "" && f.Image == nil {
		errs["share_image"] = "Изображения нет в медиатеке: выберите другое."
	}

	s.Phone = oneLine(s.Phone)
	switch {
	case utf8.RuneCountInString(s.Phone) > maxPhoneLen:
		errs["phone"] = fmt.Sprintf("Телефон длиннее %d символов.", maxPhoneLen)
	case s.Phone != "" && !strings.ContainsFunc(s.Phone, unicode.IsDigit):
		errs["phone"] = "В телефоне нет цифр."
	}
	if s.Email = strings.TrimSpace(s.Email); s.Email != "" && !validEmail(s.Email) {
		errs["email"] = "Неверный email: нужен адрес вида name@example.com."
	}
	if s.Address = multiLine(s.Address); utf8.RuneCountInString(s.Address) > maxAddressLen {
		errs["address"] = fmt.Sprintf("Адрес длиннее %d символов.", maxAddressLen)
	}

	validateSocial(s, errs)

	if s.CookieText = oneLine(s.CookieText); utf8.RuneCountInString(s.CookieText) > maxCookieTextLen {
		errs["cookie_text"] = fmt.Sprintf("Текст длиннее %d символов.", maxCookieTextLen)
	}
	if s.CookieLink = strings.TrimSpace(s.CookieLink); s.CookieLink != "" && !validLink(s.CookieLink) {
		errs["cookie_link"] = "Нужен путь на сайте (/privacy) или адрес, начинающийся с https://."
	}

	for _, v := range []struct {
		field string
		value *string
	}{{"yandex_verification", &s.Yandex}, {"google_verification", &s.Google}} {
		*v.value = verification(*v.value)
		if *v.value != "" && !verificationCode.MatchString(*v.value) {
			errs[v.field] = "Нужен код из мета-тега: латиница, цифры, «-» и «_» (можно вставить мета-тег целиком)."
		}
	}

	if len(errs) > 0 {
		f.Errors = errs
		return false
	}
	return true
}

// validateSocial нормализует ссылки на соцсети и проверяет их. Элемент без названия и адреса (добавили и не
// заполнили) отбрасывается молча; ошибки — у поля элемента (listfield.Name), общая — под ключом SocialField.
func validateSocial(s *Settings, errs map[string]string) {
	list := s.Social[:0]
	for _, it := range s.Social {
		it.Name, it.URL = oneLine(it.Name), strings.TrimSpace(it.URL)
		if it.Name == "" && it.URL == "" {
			continue
		}
		name := func(field string) string { return listfield.Name(SocialField, it.ID, field) }
		switch {
		case it.Name == "":
			errs[name("name")] = "Укажите название: как подписать ссылку."
		case utf8.RuneCountInString(it.Name) > maxSocialNameLen:
			errs[name("name")] = fmt.Sprintf("Название длиннее %d символов.", maxSocialNameLen)
		}
		switch {
		case it.URL == "":
			errs[name("url")] = "Укажите адрес страницы."
		case !validAbsURL(it.URL):
			errs[name("url")] = "Нужен адрес, начинающийся с https://."
		}
		list = append(list, it)
	}
	s.Social = list
	if len(list) > listfield.MaxItems {
		errs[SocialField] = fmt.Sprintf("Ссылок больше %d.", listfield.MaxItems)
	}
}

// verification — код подтверждения из поля: вставленный целиком мета-тег сводится к значению content.
func verification(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "<") {
		if m := metaContent.FindStringSubmatch(s); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return s
}

// validEmail — один адрес без имени и угловых скобок: он идёт в ссылку mailto:.
func validEmail(s string) bool {
	if len(s) > maxEmailLen || strings.ContainsAny(s, " <>\"") {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && a.Name == ""
}

// validLink — путь на этом сайте («/privacy», не «//host») или абсолютный адрес http(s) с хостом.
func validLink(s string) bool {
	if utf8.RuneCountInString(s) > maxLinkLen || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return !strings.HasPrefix(s, "//") && !strings.HasPrefix(s, "/\\")
	}
	return validAbsURL(s)
}

// validAbsURL — абсолютный адрес http(s) с хостом, без пробелов, не длиннее maxLinkLen.
func validAbsURL(s string) bool {
	if utf8.RuneCountInString(s) > maxLinkLen || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// describedBy — атрибуты доступности поля: подсказка и ошибка (aria-describedby), aria-invalid при ошибке.
func describedBy(f settingsForm, name string, hint bool) templ.Attributes {
	var ids []string
	if hint {
		ids = append(ids, name+"-hint")
	}
	a := templ.Attributes{}
	if f.Errors[name] != "" {
		a["aria-invalid"] = "true"
		ids = append(ids, name+"-error")
	}
	if len(ids) > 0 {
		a["aria-describedby"] = strings.Join(ids, " ")
	}
	return a
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// multiLine — текст в несколько строк: CRLF → LF, пробелы по краям строк и пустые строки убираются.
func multiLine(s string) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if l = oneLine(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// options — картинки для выбора: медиатека и выбранная, если её там нет (загружена давно).
func options(f settingsForm) []*images.Image {
	if f.Image == nil {
		return f.Library
	}
	for _, l := range f.Library {
		if l.ID == f.Image.ID {
			return f.Library
		}
	}
	return append([]*images.Image{f.Image}, f.Library...)
}

func optionLabel(img *images.Image) string {
	name := img.Name
	if name == "" {
		name = img.ID
	}
	return fmt.Sprintf("%s · %d×%d", name, img.Width, img.Height)
}
