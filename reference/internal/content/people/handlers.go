package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/a-h/templ"

	"github.com/voffkin/keelkit/modules/images"
)

// Handler — люди на сайте: своих адресов у них нет, вывод встраивает блок people (Cards).
type Handler struct {
	q      *Queries
	images Images
}

func NewHandler(db DBTX, imgs Images) *Handler {
	return &Handler{q: New(db), images: imgs}
}

// card — человек для вывода: фото загружено (nil — без фото или его нет в медиатеке).
type card struct {
	Name  string
	Role  string
	Photo *images.Image
}

// Cards — все люди в порядке из админки карточками (источник блока-списка people: blocks.ListSource).
// Людей нет — пустой вывод.
func (h *Handler) Cards(ctx context.Context) (templ.Component, error) {
	rows, err := h.q.ListPeople(ctx)
	if err != nil {
		return nil, fmt.Errorf("list people: %w", err)
	}
	cards := make([]card, 0, len(rows))
	for _, p := range rows {
		c := card{Name: p.Name, Role: p.Role}
		if p.Photo.Valid {
			c.Photo, err = h.images.Get(ctx, p.Photo.String)
			if err != nil && !errors.Is(err, images.ErrNotFound) {
				return nil, fmt.Errorf("person %d photo: %w", p.ID, err)
			}
		}
		cards = append(cards, c)
	}
	return Cards(cards), nil
}
