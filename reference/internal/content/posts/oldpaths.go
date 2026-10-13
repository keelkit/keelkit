package posts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Старые адреса поста — пути под BlogPath, с которых сайт отвечает 301 на текущий адрес поста (post_redirects).
// Туда же попадает прежний slug при публикации с новым и при снятии с публикации. Редактор вписывает старые
// адреса в форме поста — например, при переносе с другой CMS, где адреса не проходят правила slug
// (/blog/old_slug, /blog/2019/05/post.html).
const (
	maxOldPaths   = 100 // старых адресов у поста
	maxOldPathLen = 500 // символов в адресе
)

// parseOldPaths разбирает поле «Старые адреса»: по адресу в строке, путь после BlogPath/ (начало BlogPath/
// не отрезается: старый адрес мог его и содержать). Пустые строки пропускаются, повторы убираются,
// %-кодирование раскрывается (адрес из строки браузера), «/» по краям отбрасываются — так их сравнивает Show.
// Возвращает адреса или сообщение об ошибке для формы.
func parseOldPaths(text string) ([]string, string) {
	var paths []string
	for line := range strings.Lines(text) {
		in := strings.TrimSpace(line)
		if in == "" {
			continue
		}
		p, msg := oldPath(in)
		if msg != "" {
			return nil, fmt.Sprintf("«%s»: %s", in, msg)
		}
		if !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}
	if len(paths) > maxOldPaths {
		return nil, fmt.Sprintf("Старых адресов больше %d.", maxOldPaths)
	}
	return paths, ""
}

// oldPath — один старый адрес из поля формы: путь без раздела и «/» по краям; иначе — сообщение об ошибке.
func oldPath(in string) (string, string) {
	if strings.Contains(in, "://") {
		return "", "укажите путь без домена: часть адреса после " + BlogPath + "/."
	}
	if strings.ContainsAny(in, "?#") {
		return "", "без ?параметров и #якоря: сайт сравнивает только путь."
	}
	p, err := url.PathUnescape(in)
	if err != nil || !utf8.ValidString(p) || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "", "адрес с ошибкой в %-кодировании."
	}
	p = strings.Trim(p, "/")
	switch first, rest, nested := strings.Cut(p, "/"); {
	case p == "":
		return "", "нужен путь после " + BlogPath + "/."
	case utf8.RuneCountInString(p) > maxOldPathLen:
		return "", fmt.Sprintf("адрес длиннее %d символов.", maxOldPathLen)
	case nested && first == "category" && !strings.Contains(rest, "/"):
		return "", "адреса category/… заняты лентами категорий."
	}
	return p, ""
}

// checkOldPaths проверяет старые адреса по базе: адрес не должен быть адресом опубликованного поста или старым
// адресом другого поста. Slug черновика среди них допустим (возврат к прежнему адресу): при публикации он
// перестанет быть редиректом (DropRedirectToPublished). Ошибка — в f.Errors["old_paths"].
func (a *Admin) checkOldPaths(ctx context.Context, f *postForm) (bool, error) {
	for _, p := range f.oldPaths {
		title, err := a.q.PublishedSlugOwner(ctx, p)
		if err == nil {
			f.Errors = map[string]string{"old_paths": fmt.Sprintf(
				"«%s» — адрес опубликованного поста «%s»: он открывается сам, редирект не нужен.", p, title)}
			return false, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		owner, err := a.q.OldPathOwner(ctx, p)
		if err == nil && owner.PostID != f.ID {
			f.Errors = map[string]string{"old_paths": fmt.Sprintf(
				"«%s» уже ведёт на пост «%s»: сначала уберите его там.", p, owner.Title)}
			return false, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	return true, nil
}

// replaceOldPaths перезаписывает старые адреса поста (внутри транзакции q).
func replaceOldPaths(ctx context.Context, q *Queries, id int64, paths []string) error {
	if err := q.DeleteOldPaths(ctx, id); err != nil {
		return err
	}
	for _, p := range paths {
		if err := q.InsertOldPath(ctx, InsertOldPathParams{Slug: p, PostID: id}); err != nil {
			return err
		}
	}
	return nil
}
