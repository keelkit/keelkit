// Package orient поворачивает и отражает изображение по значению тега EXIF Orientation (1–8).
//
// Значение описывает, где в показанном изображении оказываются нулевая строка и нулевой столбец
// сохранённого: 6 — «строка 0 справа, столбец 0 сверху», то есть показывать с поворотом на 90° по часовой.
package orient

import "image"

// Swaps сообщает, меняются ли местами ширина и высота (ориентации 5–8).
func Swaps(o int) bool { return o >= 5 && o <= 8 }

// Apply возвращает изображение в правильной ориентации. Для 1 и неизвестных значений возвращает src.
func Apply(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if Swaps(o) {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for sy := range h {
		row := src.Pix[src.PixOffset(b.Min.X, b.Min.Y+sy):]
		for sx := range w {
			var dx, dy int
			switch o {
			case 2: // отражение по горизонтали
				dx, dy = w-1-sx, sy
			case 3: // поворот на 180°
				dx, dy = w-1-sx, h-1-sy
			case 4: // отражение по вертикали
				dx, dy = sx, h-1-sy
			case 5: // транспонирование
				dx, dy = sy, sx
			case 6: // поворот на 90° по часовой
				dx, dy = h-1-sy, sx
			case 7: // транспонирование по побочной диагонали
				dx, dy = h-1-sy, w-1-sx
			case 8: // поворот на 90° против часовой
				dx, dy = sy, w-1-sx
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):][:4], row[sx*4:sx*4+4])
		}
	}
	return dst
}
